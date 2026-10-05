// Package repositories contains all SQL. Nothing above this layer knows SQL.
package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/bhanuprataps/scaling-systems/internal/apierr"
	"github.com/bhanuprataps/scaling-systems/internal/models"
)

// UserRepository is the only place that knows the users table.
type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository { return &UserRepository{db: db} }

const userColumns = "id, name, email, created_at, updated_at"

func scanUser(row interface{ Scan(...any) error }) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (r *UserRepository) Create(ctx context.Context, name, email string) (models.User, error) {
	return instrument(ctx, "users.create", func() (models.User, error) {
		row := r.db.QueryRowContext(ctx,
			`INSERT INTO users (name, email) VALUES ($1, $2) RETURNING `+userColumns,
			name, email)
		u, err := scanUser(row)
		if err != nil {
			return models.User{}, translateError(err, "user", "user")
		}
		return u, nil
	})
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (models.User, error) {
	return instrument(ctx, "users.get", func() (models.User, error) {
		row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
		u, err := scanUser(row)
		if err != nil {
			return models.User{}, translateError(err, "user", "user")
		}
		return u, nil
	})
}

// List returns a page of users ordered by id, plus the total row count.
//
// Note: the count(*) is a full table scan on every collection request. That is
// a genuine baseline cost, not an oversight - it is exactly the kind of thing
// later experiments should notice and fix deliberately.
func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]models.User, int64, error) {
	var items []models.User
	var total int64
	err := instrumentErr(ctx, "users.list", func() error {
		if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
			return err
		}

		rows, err := r.db.QueryContext(ctx,
			`SELECT `+userColumns+` FROM users ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
		if err != nil {
			return translateError(err, "user", "user")
		}
		defer func() { _ = rows.Close() }()

		items = make([]models.User, 0, limit)
		for rows.Next() {
			item, scanErr := scanUser(rows)
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

// Update applies a partial update. A nil field is left untouched.
func (r *UserRepository) Update(ctx context.Context, id int64, name, email *string) (models.User, error) {
	return instrument(ctx, "users.update", func() (models.User, error) {
		row := r.db.QueryRowContext(ctx, `
			UPDATE users
			   SET name       = COALESCE($2, name),
			       email      = COALESCE($3, email),
			       updated_at = now()
			 WHERE id = $1
			RETURNING `+userColumns,
			id, name, email)
		u, err := scanUser(row)
		if err != nil {
			return models.User{}, translateError(err, "user", "user")
		}
		return u, nil
	})
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	return instrumentErr(ctx, "users.delete", func() error {
		res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
		if err != nil {
			return translateError(err, "user", "user")
		}
		return requireAffected(res, "user", id)
	})
}

// Exists reports whether a user id is present. Used by the order service to
// return a friendly error before the FK constraint rejects the insert.
func (r *UserRepository) Exists(ctx context.Context, id int64) (bool, error) {
	return instrument(ctx, "users.exists", func() (bool, error) {
		var exists bool
		err := r.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, id).Scan(&exists)
		return exists, err
	})
}

func requireAffected(res sql.Result, resource string, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound(resource+"_not_found", fmt.Sprintf("%s %d does not exist", resource, id))
	}
	return nil
}
