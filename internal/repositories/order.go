package repositories

import (
	"context"
	"database/sql"

	"github.com/bhanuprataps/scaling-systems/internal/models"
)

// OrderRepository is the only place that knows the orders table.
type OrderRepository struct {
	db *sql.DB
}

func NewOrderRepository(db *sql.DB) *OrderRepository { return &OrderRepository{db: db} }

const orderColumns = "id, user_id, status, total_amount, created_at, updated_at"

func scanOrder(row interface{ Scan(...any) error }) (models.Order, error) {
	var o models.Order
	err := row.Scan(&o.ID, &o.UserID, &o.Status, &o.TotalAmount, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

func (r *OrderRepository) Create(ctx context.Context, userID int64, status, totalAmount string) (models.Order, error) {
	return instrument(ctx, "orders.create", func() (models.Order, error) {
		row := r.db.QueryRowContext(ctx,
			`INSERT INTO orders (user_id, status, total_amount) VALUES ($1, $2, $3) RETURNING `+orderColumns,
			userID, status, totalAmount)
		o, err := scanOrder(row)
		if err != nil {
			return models.Order{}, translateError(err, "order", "user")
		}
		return o, nil
	})
}

func (r *OrderRepository) GetByID(ctx context.Context, id int64) (models.Order, error) {
	return instrument(ctx, "orders.get", func() (models.Order, error) {
		row := r.db.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = $1`, id)
		o, err := scanOrder(row)
		if err != nil {
			return models.Order{}, translateError(err, "order", "user")
		}
		return o, nil
	})
}

// List returns a page of orders ordered by id plus the total row count.
//
// There is deliberately no index on orders.user_id and no supporting index
// for `ORDER BY id` beyond the primary key. The orders table is the largest
// (500k rows) so this endpoint is expected to be a baseline bottleneck.
func (r *OrderRepository) List(ctx context.Context, limit, offset int) ([]models.Order, int64, error) {
	var items []models.Order
	var total int64
	err := instrumentErr(ctx, "orders.list", func() error {
		if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM orders`).Scan(&total); err != nil {
			return err
		}

		rows, err := r.db.QueryContext(ctx,
			`SELECT `+orderColumns+` FROM orders ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
		if err != nil {
			return translateError(err, "orders", "orders")
		}
		defer func() { _ = rows.Close() }()

		items = make([]models.Order, 0, limit)
		for rows.Next() {
			item, scanErr := scanOrder(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *OrderRepository) Update(ctx context.Context, id int64, status, totalAmount *string) (models.Order, error) {
	return instrument(ctx, "orders.update", func() (models.Order, error) {
		row := r.db.QueryRowContext(ctx, `
			UPDATE orders
			   SET status       = COALESCE($2, status),
			       total_amount = COALESCE($3, total_amount),
			       updated_at   = now()
			 WHERE id = $1
			RETURNING `+orderColumns,
			id, status, totalAmount)
		o, err := scanOrder(row)
		if err != nil {
			return models.Order{}, translateError(err, "order", "user")
		}
		return o, nil
	})
}

func (r *OrderRepository) Delete(ctx context.Context, id int64) error {
	return instrumentErr(ctx, "orders.delete", func() error {
		res, err := r.db.ExecContext(ctx, `DELETE FROM orders WHERE id = $1`, id)
		if err != nil {
			return translateError(err, "order", "user")
		}
		return requireAffected(res, "order", id)
	})
}
