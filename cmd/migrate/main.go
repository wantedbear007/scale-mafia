// Command migrate applies database migrations and optionally seeds benchmark
// data. Both operations are idempotent, so it is safe to re-run.
//
// Usage:
//
//	migrate up           apply pending migrations
//	migrate status       show applied / pending migrations
//	migrate seed         seed benchmark data (respects SEED_* env vars)
//	migrate seed -force  delete existing data first, then seed
//	migrate reset        drop everything and re-apply migrations
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bhanuprataps/scaling-systems/internal/config"
	"github.com/bhanuprataps/scaling-systems/internal/database"
	"github.com/bhanuprataps/scaling-systems/internal/migrate"
	"github.com/bhanuprataps/scaling-systems/internal/observability"
	"github.com/bhanuprataps/scaling-systems/internal/seeder"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	command := os.Args[1]

	fs := flag.NewFlagSet(command, flag.ExitOnError)
	force := fs.Bool("force", false, "delete all existing rows before seeding (seed only)")
	_ = fs.Parse(os.Args[2:])

	switch command {
	case "up", "status", "seed", "reset", "version":
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		usage()
		os.Exit(2)
	}

	if err := run(command, *force); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `migrate - database migrations and benchmark seeding

Commands:
  up            apply all pending migrations
  status        list applied and pending migrations
  seed          insert benchmark data (skips work if already seeded)
  seed -force   delete all existing data first, then insert benchmark data
  reset         drop all tables, then re-apply migrations
  version       print version
`)
}

func run(command string, force bool) error {
	if command == "version" {
		fmt.Println("migrate", version)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := observability.NewLogger(observability.LogConfig{
		Level:  parseLevel(cfg.Log.Level),
		Format: cfg.Log.Format,
		Env:    cfg.App.Env,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch command {
	case "up":
		return applyMigrations(ctx, db, log)
	case "status":
		return showStatus(ctx, db)
	case "reset":
		log.Warn("resetting database", "dsn", cfg.Database.Redacted())
		if _, err := db.ExecContext(ctx,
			`DROP TABLE IF EXISTS orders, products, users, schema_migrations CASCADE`); err != nil {
			return fmt.Errorf("drop tables: %w", err)
		}
		return applyMigrations(ctx, db, log)
	case "seed":
		return seeder.New(db, log).Run(ctx, seeder.Options{
			Users:     cfg.Seed.Users,
			Products:  cfg.Seed.Products,
			Orders:    cfg.Seed.Orders,
			BatchSize: cfg.Seed.BatchSize,
			Force:     force || cfg.Seed.Force,
		})
	}
	return nil
}

func applyMigrations(ctx context.Context, db *sql.DB, log *slog.Logger) error {
	start := time.Now()
	res, err := migrate.Run(ctx, db)
	if err != nil {
		return err
	}
	if len(res.Applied) == 0 {
		log.Info("migrations_up_to_date", "skipped", res.Skipped, "elapsed", time.Since(start).String())
	} else {
		log.Info("migrations_applied", "applied", res.Applied, "skipped", res.Skipped, "elapsed", time.Since(start).String())
	}
	return nil
}

func showStatus(ctx context.Context, db *sql.DB) error {
	all, err := migrate.Load()
	if err != nil {
		return err
	}

	rows, err := db.QueryContext(ctx, "SELECT version, name, applied_at FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read schema_migrations (has 'up' been run?): %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := map[int64]time.Time{}
	for rows.Next() {
		var v int64
		var name string
		var at time.Time
		if err := rows.Scan(&v, &name, &at); err != nil {
			return err
		}
		applied[v] = at
	}
	if err := rows.Err(); err != nil {
		return err
	}

	fmt.Printf("%-10s %-24s %-12s %s\n", "VERSION", "NAME", "STATE", "APPLIED AT")
	for _, m := range all {
		if at, ok := applied[m.Version]; ok {
			fmt.Printf("%-10d %-24s %-12s %s\n", m.Version, m.Name, "applied", at.UTC().Format(time.RFC3339))
		} else {
			fmt.Printf("%-10d %-24s %-12s %s\n", m.Version, m.Name, "pending", "-")
		}
	}
	return nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
