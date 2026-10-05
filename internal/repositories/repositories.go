package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bhanuprataps/scaling-systems/internal/apierr"
	"github.com/bhanuprataps/scaling-systems/internal/observability"
)

// SQLSTATE codes we translate into application errors.
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
)

// instrument records the duration and outcome of one SQL round trip.
//
// `operation` must be a small, fixed, low-cardinality label such as
// "users.list" - never a value derived from user input.
func instrument[T any](ctx context.Context, operation string, fn func() (T, error)) (T, error) {
	start := time.Now()
	res, err := fn()
	elapsed := time.Since(start).Seconds()

	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	observability.ObserveQuery(operation, outcome, elapsed)
	return res, err
}

// instrumentErr is the void-returning variant of instrument.
func instrumentErr(ctx context.Context, operation string, fn func() error) error {
	start := time.Now()
	err := fn()
	elapsed := time.Since(start).Seconds()

	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	observability.ObserveQuery(operation, outcome, elapsed)
	return err
}

// translateError converts driver-specific errors into application errors so
// handlers never need to know about PostgreSQL internals.
func translateError(err error, resource, referenced string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return apierr.Conflict("duplicate_"+resource,
				fmt.Sprintf("a %s with these unique values already exists", resource))
		case foreignKeyViolation:
			return apierr.BadRequest("invalid_foreign_key",
				fmt.Sprintf("referenced %s does not exist", referenced))
		case checkViolation:
			return apierr.BadRequest("constraint_violation",
				"the request violates a database constraint")
		}
	}
	return err
}
