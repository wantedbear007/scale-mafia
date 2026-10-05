// Package seeder inserts a deterministic benchmark dataset.
//
// It uses batched multi-row INSERTs through database/sql. COPY would be
// faster, but batching keeps the seeder on the exact same driver and code
// path as the application, which matters more than shaving seconds off a
// one-off setup step.
//
// The dataset is deterministic: run it twice on an empty database and you get
// the same values, which keeps benchmark runs comparable.
package seeder

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/bhanuprataps/scaling-systems/internal/models"
)

type Options struct {
	Users     int
	Products  int
	Orders    int
	BatchSize int
	Force     bool
}

type Seeder struct {
	db  *sql.DB
	log *slog.Logger
}

func New(db *sql.DB, log *slog.Logger) *Seeder { return &Seeder{db: db, log: log} }

// Run seeds the database. Without Force it is a no-op when the tables already
// contain the requested number of rows.
func (s *Seeder) Run(ctx context.Context, opt Options) error {
	start := time.Now()
	if opt.BatchSize < 1 {
		opt.BatchSize = 1_000
	}

	if opt.Force {
		s.log.Warn("force_seed_deleting_existing_rows")
		// Single statement: TRUNCATE ... RESTART IDENTITY CASCADE.
		if _, err := s.db.ExecContext(ctx,
			`TRUNCATE orders, products, users RESTART IDENTITY CASCADE`); err != nil {
			return fmt.Errorf("truncate: %w", err)
		}
	}

	users, err := s.tableStats(ctx, "users")
	if err != nil {
		return err
	}
	products, err := s.tableStats(ctx, "products")
	if err != nil {
		return err
	}
	orders, err := s.tableStats(ctx, "orders")
	if err != nil {
		return err
	}
	s.log.Info("current_dataset",
		"users", users, "products", products, "orders", orders,
		"target_users", opt.Users, "target_products", opt.Products, "target_orders", opt.Orders)

	toUsers := max(opt.Users-users.Count, 0)
	toProducts := max(opt.Products-products.Count, 0)
	toOrders := max(opt.Orders-orders.Count, 0)

	// Snapshot the starting row counts for the final report; `users` is
	// re-read after seeding and must not be used for the total.
	startUsers, startProducts, startOrders := users.Count, products.Count, orders.Count

	if toUsers == 0 && toProducts == 0 && toOrders == 0 {
		s.log.Info("seed_skipped_dataset_already_present", "elapsed", time.Since(start).String())
		return nil
	}

	if err := s.assertUsable(users, products, orders); err != nil {
		return err
	}

	if toUsers > 0 {
		if err := s.seedUsers(ctx, users.MaxID, toUsers, opt.BatchSize); err != nil {
			return err
		}
		// Re-read: orders must reference the user ids that now exist.
		if users, err = s.tableStats(ctx, "users"); err != nil {
			return err
		}
	}
	if toProducts > 0 {
		if err := s.seedProducts(ctx, products.MaxID, toProducts, opt.BatchSize); err != nil {
			return err
		}
	}
	if toOrders > 0 {
		// Orders reference the user id space, which after a clean seed is 1..MaxID.
		if err := s.seedOrders(ctx, orders.MaxID, users.MaxID, toOrders, opt.BatchSize); err != nil {
			return err
		}
	}

	// The seeder inserts explicit ids so the dataset is fully deterministic
	// (k6 can then address user 1..N). Because those inserts bypass the
	// identity sequence, fast-forward each sequence to match.
	if err := s.syncSequences(ctx); err != nil {
		return err
	}

	// Refresh planner statistics so benchmarks are not distorted by stale
	// estimates on a freshly bulk-loaded table.
	if _, err := s.db.ExecContext(ctx, "ANALYZE users, products, orders"); err != nil {
		s.log.Warn("analyze_failed", "error", err)
	}

	s.log.Info("seed_complete",
		"users", startUsers+toUsers, "products", startProducts+toProducts, "orders", startOrders+toOrders,
		"elapsed", time.Since(start).String())
	return nil
}

// tableStats reports both the row count and the highest id of a table. Ids are
// assigned from MaxID so appending to a table that has had rows deleted (for
// example during a benchmark run) can never collide with an existing id.
type tableState struct {
	Count int
	MaxID int64
}

// syncSequences points each identity sequence at the current maximum id.
func (s *Seeder) syncSequences(ctx context.Context) error {
	for _, table := range []string{"users", "products", "orders"} {
		stmt := fmt.Sprintf(
			`SELECT setval(pg_get_serial_sequence('%s', 'id'),
			               COALESCE((SELECT max(id) FROM %s), 1),
			               EXISTS (SELECT 1 FROM %s))`, table, table, table)
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sync sequence for %s: %w", table, err)
		}
	}
	return nil
}

// assertUsable refuses to seed orders when no users exist, since the foreign
// key would make every insert fail.
func (s *Seeder) assertUsable(users, products, orders tableState) error {
	if orders.Count > 0 && users.Count == 0 {
		return fmt.Errorf(
			"orders exist (%d rows) but users is empty; "+
				"re-run with -force (or `make db-reset`) to rebuild the dataset deterministically",
			orders.Count)
	}
	return nil
}

func (s *Seeder) tableStats(ctx context.Context, table string) (tableState, error) {
	var st tableState
	// table is a hard-coded literal from this package, never user input.
	stmt := fmt.Sprintf("SELECT count(*), COALESCE(max(id), 0) FROM %s", table)
	if err := s.db.QueryRowContext(ctx, stmt).Scan(&st.Count, &st.MaxID); err != nil {
		return st, fmt.Errorf("stats for %s: %w", table, err)
	}
	return st, nil
}

func (s *Seeder) seedUsers(ctx context.Context, existing int64, total, batch int) error {
	rng := rand.New(rand.NewSource(42))
	start := time.Now()
	s.log.Info("seeding_users", "from", existing, "count", total, "batch_size", batch)

	for done := 0; done < total; {
		n := min(batch, total-done)
		var b strings.Builder
		b.WriteString(`INSERT INTO users (id, name, email, created_at, updated_at) VALUES `)
		args := make([]any, 0, n*3)
		for i := 0; i < n; i++ {
			id := existing + int64(done+i) + 1
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `($%d,$%d,$%d,now(),now())`, i*3+1, i*3+2, i*3+3)
			args = append(args, id, fakeName(rng, id), fmt.Sprintf("user%d@example.com", id))
		}
		if _, err := s.db.ExecContext(ctx, b.String(), args...); err != nil {
			return fmt.Errorf("insert users batch at %d: %w", existing+int64(done), err)
		}
		done += n
	}
	s.log.Info("seeded_users", "rows", total, "elapsed", time.Since(start).String())
	return nil
}

func (s *Seeder) seedProducts(ctx context.Context, existing int64, total, batch int) error {
	rng := rand.New(rand.NewSource(7))
	start := time.Now()
	s.log.Info("seeding_products", "from", existing, "count", total, "batch_size", batch)

	for done := 0; done < total; {
		n := min(batch, total-done)
		var b strings.Builder
		b.WriteString(`INSERT INTO products (id, name, description, price, stock, created_at, updated_at) VALUES `)
		args := make([]any, 0, n*4)
		for i := 0; i < n; i++ {
			id := existing + int64(done+i) + 1
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `($%d,$%d,$%d,$%d,$%d,now(),now())`, i*5+1, i*5+2, i*5+3, i*5+4, i*5+5)
			price := float64(rng.Intn(20000)+99) / 100.0 // 0.99 .. 200.98
			args = append(args, id, fakeProductName(rng, id),
				fmt.Sprintf("Benchmark product %d", id),
				fmt.Sprintf("%.2f", price), rng.Intn(1000))
		}
		if _, err := s.db.ExecContext(ctx, b.String(), args...); err != nil {
			return fmt.Errorf("insert products batch at %d: %w", existing+int64(done), err)
		}
		done += n
	}
	s.log.Info("seeded_products", "rows", total, "elapsed", time.Since(start).String())
	return nil
}

func (s *Seeder) seedOrders(ctx context.Context, existing, userMaxID int64, total, batch int) error {
	rng := rand.New(rand.NewSource(1337))
	start := time.Now()
	s.log.Info("seeding_orders", "from", existing, "count", total, "batch_size", batch,
		"user_max_id", userMaxID)

	// 5/5/5/5/80 split keeps the mix realistic: most orders are in flight.
	statuses := []string{
		models.OrderStatusPending, models.OrderStatusPending, models.OrderStatusPending,
		models.OrderStatusPending, models.OrderStatusPending,
		models.OrderStatusPaid, models.OrderStatusPaid, models.OrderStatusPaid, models.OrderStatusPaid,
		models.OrderStatusShipped, models.OrderStatusShipped, models.OrderStatusShipped,
		models.OrderStatusDelivered, models.OrderStatusDelivered, models.OrderStatusDelivered,
		models.OrderStatusDelivered, models.OrderStatusDelivered, models.OrderStatusDelivered,
		models.OrderStatusDelivered, models.OrderStatusDelivered, models.OrderStatusDelivered,
		models.OrderStatusCancelled, models.OrderStatusCancelled, models.OrderStatusCancelled,
	}

	if userMaxID < 1 {
		return fmt.Errorf("cannot seed orders: no users exist")
	}

	for done := 0; done < total; {
		n := min(batch, total-done)
		var b strings.Builder
		b.WriteString(`INSERT INTO orders (id, user_id, status, total_amount, created_at, updated_at) VALUES `)
		args := make([]any, 0, n*4)
		for i := 0; i < n; i++ {
			id := existing + int64(done+i) + 1
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `($%d,$%d,$%d,$%d,now(),now())`, i*4+1, i*4+2, i*4+3, i*4+4)
			args = append(args,
				id,
				int64(rng.Intn(int(userMaxID))+1),
				statuses[rng.Intn(len(statuses))],
				fmt.Sprintf("%.2f", float64(rng.Intn(50000)+100)/100.0),
			)
		}
		if _, err := s.db.ExecContext(ctx, b.String(), args...); err != nil {
			return fmt.Errorf("insert orders batch at %d: %w", existing+int64(done), err)
		}
		done += n
	}
	s.log.Info("seeded_orders", "rows", total, "elapsed", time.Since(start).String())
	return nil
}

var firstNames = []string{
	"ada", "grace", "alan", "linus", "barbara", "ken", "dennis", "margaret",
	"edsger", "donald", "niklaus", "leslie", "john", "tony", "hedy", "claude",
}

var productAdjectives = []string{
	"wireless", "compact", "premium", "budget", "portable", "ergonomic", "durable", "smart",
}

var productNouns = []string{
	"keyboard", "monitor", "mouse", "headset", "router", "laptop-stand", "webcam", "dock",
}

func fakeName(rng *rand.Rand, id int64) string {
	return fmt.Sprintf("%s %s %d",
		firstNames[rng.Intn(len(firstNames))],
		productNouns[rng.Intn(len(productNouns))],
		id)
}

func fakeProductName(rng *rand.Rand, id int64) string {
	return fmt.Sprintf("%s %s %d",
		productAdjectives[rng.Intn(len(productAdjectives))],
		productNouns[rng.Intn(len(productNouns))],
		id)
}
