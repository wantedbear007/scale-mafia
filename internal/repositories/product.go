package repositories

import (
	"context"
	"database/sql"

	"github.com/bhanuprataps/scaling-systems/internal/models"
)

// ProductRepository is the only place that knows the products table.
type ProductRepository struct {
	db *sql.DB
}

func NewProductRepository(db *sql.DB) *ProductRepository { return &ProductRepository{db: db} }

const productColumns = "id, name, description, price, stock, created_at, updated_at"

func scanProduct(row interface{ Scan(...any) error }) (models.Product, error) {
	var p models.Product
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (r *ProductRepository) Create(ctx context.Context, name, description, price string, stock int32) (models.Product, error) {
	return instrument(ctx, "products.create", func() (models.Product, error) {
		row := r.db.QueryRowContext(ctx,
			`INSERT INTO products (name, description, price, stock) VALUES ($1, $2, $3, $4) RETURNING `+productColumns,
			name, description, price, stock)
		p, err := scanProduct(row)
		if err != nil {
			return models.Product{}, translateError(err, "product", "product")
		}
		return p, nil
	})
}

func (r *ProductRepository) GetByID(ctx context.Context, id int64) (models.Product, error) {
	return instrument(ctx, "products.get", func() (models.Product, error) {
		row := r.db.QueryRowContext(ctx, `SELECT `+productColumns+` FROM products WHERE id = $1`, id)
		p, err := scanProduct(row)
		if err != nil {
			return models.Product{}, translateError(err, "product", "product")
		}
		return p, nil
	})
}

func (r *ProductRepository) List(ctx context.Context, limit, offset int) ([]models.Product, int64, error) {
	var items []models.Product
	var total int64
	err := instrumentErr(ctx, "products.list", func() error {
		if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM products`).Scan(&total); err != nil {
			return err
		}

		rows, err := r.db.QueryContext(ctx,
			`SELECT `+productColumns+` FROM products ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
		if err != nil {
			return translateError(err, "product", "product")
		}
		defer func() { _ = rows.Close() }()

		items = make([]models.Product, 0, limit)
		for rows.Next() {
			item, scanErr := scanProduct(rows)
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

func (r *ProductRepository) Update(ctx context.Context, id int64, name, description, price *string, stock *int32) (models.Product, error) {
	return instrument(ctx, "products.update", func() (models.Product, error) {
		row := r.db.QueryRowContext(ctx, `
			UPDATE products
			   SET name        = COALESCE($2, name),
			       description = COALESCE($3, description),
			       price       = COALESCE($4, price),
			       stock       = COALESCE($5, stock),
			       updated_at  = now()
			 WHERE id = $1
			RETURNING `+productColumns,
			id, name, description, price, stock)
		p, err := scanProduct(row)
		if err != nil {
			return models.Product{}, translateError(err, "product", "product")
		}
		return p, nil
	})
}

func (r *ProductRepository) Delete(ctx context.Context, id int64) error {
	return instrumentErr(ctx, "products.delete", func() error {
		res, err := r.db.ExecContext(ctx, `DELETE FROM products WHERE id = $1`, id)
		if err != nil {
			return translateError(err, "product", "product")
		}
		return requireAffected(res, "product", id)
	})
}
