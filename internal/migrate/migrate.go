// Package migrate applies the embedded SQL migrations to PostgreSQL.
//
// The runner is intentionally minimal and deterministic:
//   - migrations are embedded in the binary (see the top-level migrations package)
//   - files are applied in lexicographic order in a single transaction each
//   - applied versions are recorded in schema_migrations
//   - a global advisory lock prevents concurrent migrators from racing
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bhanuprataps/scaling-systems/migrations"
)

// advisoryLockID is an arbitrary constant used with pg_advisory_lock so that
// only one migrator mutates the schema at a time.
const advisoryLockID int64 = 8_090_113_201

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     BIGINT PRIMARY KEY,
    name        TEXT        NOT NULL,
    checksum    TEXT        NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms INTEGER     NOT NULL
);`

type Migration struct {
	Version  int64
	Name     string
	Checksum string
	SQL      string
}

type Result struct {
	Applied []int64
	Skipped []int64
}

// Load reads and parses all embedded migration files.
func Load() ([]Migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// 0001_init.sql -> version 1, name "init"
		parts := strings.SplitN(strings.TrimSuffix(e.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration %q must be named <version>_<name>.sql", e.Name())
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration %q has an invalid version: %w", e.Name(), err)
		}
		body, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		out = append(out, Migration{
			Version:  version,
			Name:     parts[1],
			Checksum: checksum(body),
			SQL:      string(body),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Run applies every pending migration and returns what was applied/skipped.
func Run(ctx context.Context, db *sql.DB) (Result, error) {
	all, err := Load()
	if err != nil {
		return Result{}, err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return Result{}, fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", advisoryLockID)
	}()

	if _, err := conn.ExecContext(ctx, createMigrationsTable); err != nil {
		return Result{}, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[int64]string{}
	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return Result{}, fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int64
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = sum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()

	var res Result
	for _, m := range all {
		if prev, ok := applied[m.Version]; ok {
			if prev != m.Checksum {
				return Result{}, fmt.Errorf(
					"migration %d_%s was already applied with a different checksum (recorded=%s current=%s); "+
						"migrations are immutable - add a new file instead", m.Version, m.Name, prev, m.Checksum)
			}
			res.Skipped = append(res.Skipped, m.Version)
			continue
		}

		start := time.Now()
		if err := applyOne(ctx, conn, m); err != nil {
			return Result{}, err
		}
		_ = start
		res.Applied = append(res.Applied, m.Version)
	}
	return res, nil
}

func applyOne(ctx context.Context, conn *sql.Conn, m Migration) error {
	start := time.Now()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d_%s: %w", m.Version, m.Name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("apply migration %d_%s: %w", m.Version, m.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, checksum, duration_ms) VALUES ($1, $2, $3, $4)",
		m.Version, m.Name, m.Checksum, int(time.Since(start).Milliseconds())); err != nil {
		return fmt.Errorf("record migration %d_%s: %w", m.Version, m.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d_%s: %w", m.Version, m.Name, err)
	}
	return nil
}

// checksum is a small FNV-1a hash - enough to detect accidental edits to
// already-applied migrations without pulling in a hashing dependency.
func checksum(b []byte) string {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var h uint64 = offset64
	for _, c := range b {
		h ^= uint64(c)
		h *= prime64
	}
	return strconv.FormatUint(h, 16)
}
