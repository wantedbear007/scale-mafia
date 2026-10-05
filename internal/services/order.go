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

type OrderService struct {
	orders *repositories.OrderRepository
	users  *repositories.UserRepository
}

func NewOrderService(orders *repositories.OrderRepository, users *repositories.UserRepository) *OrderService {
	return &OrderService{orders: orders, users: users}
}

func (s *OrderService) Create(ctx context.Context, req models.CreateOrderRequest) (models.Order, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "user_id", req.UserID)
	status := models.OrderStatusPending
	if req.Status != nil {
		status = validation.OneOf(c, "status", *req.Status, models.OrderStatuses)
	}
	total := validation.Money(c, "total_amount", req.TotalAmount, true)
	if err := c.Err(); err != nil {
		return models.Order{}, validationErr(err)
	}

	// The FK constraint is the real guarantee; this check only produces a
	// nicer 400 (and a clearer log line) for the common mistake.
	exists, err := s.users.Exists(ctx, req.UserID)
	if err != nil {
		return models.Order{}, err
	}
	if !exists {
		return models.Order{}, apierr.BadRequest("invalid_foreign_key",
			fmt.Sprintf("referenced user does not exist (user_id=%d)", req.UserID))
	}

	return s.orders.Create(ctx, req.UserID, status, total)
}

func (s *OrderService) GetByID(ctx context.Context, id int64) (models.Order, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)
	if err := c.Err(); err != nil {
		return models.Order{}, validationErr(err)
	}

	o, err := s.orders.GetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Order{}, apierr.NotFound("order_not_found", fmt.Sprintf("order %d does not exist", id))
	}
	return o, err
}

func (s *OrderService) List(ctx context.Context, page, limit int) (models.Collection[models.Order], error) {
	offset := (page - 1) * limit
	orders, total, err := s.orders.List(ctx, limit, offset)
	if err != nil {
		return models.Collection[models.Order]{}, err
	}
	return models.Collection[models.Order]{
		Data:       orders,
		Pagination: models.Pagination{Page: page, Limit: limit}.Compute(total),
	}, nil
}

func (s *OrderService) Update(ctx context.Context, id int64, req models.UpdateOrderRequest) (models.Order, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)

	var status, total *string
	if req.Status != nil {
		v := validation.OneOf(c, "status", *req.Status, models.OrderStatuses)
		status = &v
	}
	if req.TotalAmount != nil {
		v := validation.Money(c, "total_amount", *req.TotalAmount, true)
		total = &v
	}
	if status == nil && total == nil {
		c.Add("body", "at least one of 'status' or 'total_amount' must be provided")
	}
	if err := c.Err(); err != nil {
		return models.Order{}, validationErr(err)
	}

	o, err := s.orders.Update(ctx, id, status, total)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Order{}, apierr.NotFound("order_not_found", fmt.Sprintf("order %d does not exist", id))
	}
	return o, err
}

func (s *OrderService) Delete(ctx context.Context, id int64) error {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)
	if err := c.Err(); err != nil {
		return validationErr(err)
	}
	return s.orders.Delete(ctx, id)
}
