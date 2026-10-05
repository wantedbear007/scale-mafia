// Package models contains the domain entities and API payload types.
package models

import (
	"strings"
	"time"
)

// OrderStatus values are constrained by a CHECK constraint in PostgreSQL.
const (
	OrderStatusPending   = "pending"
	OrderStatusPaid      = "paid"
	OrderStatusShipped   = "shipped"
	OrderStatusDelivered = "delivered"
	OrderStatusCancelled = "cancelled"
)

var OrderStatuses = []string{
	OrderStatusPending,
	OrderStatusPaid,
	OrderStatusShipped,
	OrderStatusDelivered,
	OrderStatusCancelled,
}

func IsValidOrderStatus(s string) bool {
	for _, v := range OrderStatuses {
		if v == s {
			return true
		}
	}
	return false
}

type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Product struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"` // NUMERIC is returned as a string to avoid float drift
	Stock       int32  `json:"stock"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Order struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Status      string `json:"status"`
	TotalAmount string `json:"total_amount"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UpdateUserRequest struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

type CreateProductRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       string  `json:"price"`
	Stock       int32   `json:"stock"`
}

type UpdateProductRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Price       *string `json:"price"`
	Stock       *int32  `json:"stock"`
}

type CreateOrderRequest struct {
	UserID      int64   `json:"user_id"`
	Status      *string `json:"status"`
	TotalAmount string  `json:"total_amount"`
}

type UpdateOrderRequest struct {
	Status      *string `json:"status"`
	TotalAmount *string `json:"total_amount"`
}

// Pagination is the metadata returned with every collection response.
type Pagination struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int64 `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
}

type Collection[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

func (p Pagination) Compute(total int64) Pagination {
	p.Total = total
	if p.Limit > 0 {
		p.TotalPages = (total + int64(p.Limit) - 1) / int64(p.Limit)
	}
	p.HasNext = int64(p.Page)*int64(p.Limit) < total
	return p
}

// NormalizeEmail trims and lowercases an email for storage/comparison.
func NormalizeEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}
