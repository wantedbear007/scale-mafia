// Package services contains business rules. Services validate input, call a
// single repository, and translate "no rows" into 404s. They do not know
// about HTTP or SQL.
package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bhanuprataps/scaling-systems/internal/apierr"
	"github.com/bhanuprataps/scaling-systems/internal/models"
	"github.com/bhanuprataps/scaling-systems/internal/repositories"
	"github.com/bhanuprataps/scaling-systems/internal/validation"
)

type UserService struct {
	users *repositories.UserRepository
}

func NewUserService(users *repositories.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Create(ctx context.Context, req models.CreateUserRequest) (models.User, error) {
	c := &validation.Collector{}
	name := validation.RequiredString(c, "name", req.Name, validation.MaxNameLength)
	email := validation.Email(c, "email", req.Email)
	if err := c.Err(); err != nil {
		return models.User{}, validationErr(err)
	}
	return s.users.Create(ctx, name, email)
}

func (s *UserService) GetByID(ctx context.Context, id int64) (models.User, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)
	if err := c.Err(); err != nil {
		return models.User{}, validationErr(err)
	}

	u, err := s.users.GetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, apierr.NotFound("user_not_found", fmt.Sprintf("user %d does not exist", id))
	}
	return u, err
}

func (s *UserService) List(ctx context.Context, page, limit int) (models.Collection[models.User], error) {
	offset := (page - 1) * limit
	users, total, err := s.users.List(ctx, limit, offset)
	if err != nil {
		return models.Collection[models.User]{}, err
	}
	return models.Collection[models.User]{
		Data:       users,
		Pagination: models.Pagination{Page: page, Limit: limit}.Compute(total),
	}, nil
}

func (s *UserService) Update(ctx context.Context, id int64, req models.UpdateUserRequest) (models.User, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)

	var name, email *string
	if req.Name != nil {
		name = validation.OptionalString(c, "name", req.Name, validation.MaxNameLength)
	}
	if req.Email != nil {
		e := validation.Email(c, "email", *req.Email)
		email = &e
	}
	if name == nil && email == nil {
		c.Add("body", "at least one of 'name' or 'email' must be provided")
	}
	if err := c.Err(); err != nil {
		return models.User{}, validationErr(err)
	}

	u, err := s.users.Update(ctx, id, name, email)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, apierr.NotFound("user_not_found", fmt.Sprintf("user %d does not exist", id))
	}
	return u, err
}

func (s *UserService) Delete(ctx context.Context, id int64) error {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)
	if err := c.Err(); err != nil {
		return validationErr(err)
	}
	return s.users.Delete(ctx, id)
}
