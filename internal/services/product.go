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

// validationErr converts a validation error into a 400 with field details.
func validationErr(err error) error {
	var ve *validation.Error
	if errors.As(err, &ve) {
		return ve.ToAPIError()
	}
	return err
}

type ProductService struct {
	products *repositories.ProductRepository
}

func NewProductService(products *repositories.ProductRepository) *ProductService {
	return &ProductService{products: products}
}

func (s *ProductService) Create(ctx context.Context, req models.CreateProductRequest) (models.Product, error) {
	c := &validation.Collector{}
	name := validation.RequiredString(c, "name", req.Name, validation.MaxNameLength)
	description := ""
	if req.Description != nil {
		description = *validation.OptionalString(c, "description", req.Description, validation.MaxDescription)
	}
	price := validation.Money(c, "price", req.Price, true)
	validation.Int32NonNegative(c, "stock", req.Stock)
	if err := c.Err(); err != nil {
		return models.Product{}, validationErr(err)
	}
	return s.products.Create(ctx, name, description, price, req.Stock)
}

func (s *ProductService) GetByID(ctx context.Context, id int64) (models.Product, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)
	if err := c.Err(); err != nil {
		return models.Product{}, validationErr(err)
	}

	p, err := s.products.GetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Product{}, apierr.NotFound("product_not_found", fmt.Sprintf("product %d does not exist", id))
	}
	return p, err
}

func (s *ProductService) List(ctx context.Context, page, limit int) (models.Collection[models.Product], error) {
	offset := (page - 1) * limit
	products, total, err := s.products.List(ctx, limit, offset)
	if err != nil {
		return models.Collection[models.Product]{}, err
	}
	return models.Collection[models.Product]{
		Data:       products,
		Pagination: models.Pagination{Page: page, Limit: limit}.Compute(total),
	}, nil
}

func (s *ProductService) Update(ctx context.Context, id int64, req models.UpdateProductRequest) (models.Product, error) {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)

	var name, description, price *string
	var stock *int32
	if req.Name != nil {
		name = validation.OptionalString(c, "name", req.Name, validation.MaxNameLength)
	}
	if req.Description != nil {
		description = validation.OptionalString(c, "description", req.Description, validation.MaxDescription)
	}
	if req.Price != nil {
		p := validation.Money(c, "price", *req.Price, true)
		price = &p
	}
	if req.Stock != nil {
		validation.Int32NonNegative(c, "stock", *req.Stock)
		stock = req.Stock
	}
	if name == nil && description == nil && price == nil && stock == nil {
		c.Add("body", "at least one updatable field must be provided")
	}
	if err := c.Err(); err != nil {
		return models.Product{}, validationErr(err)
	}

	p, err := s.products.Update(ctx, id, name, description, price, stock)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Product{}, apierr.NotFound("product_not_found", fmt.Sprintf("product %d does not exist", id))
	}
	return p, err
}

func (s *ProductService) Delete(ctx context.Context, id int64) error {
	c := &validation.Collector{}
	validation.Int64ID(c, "id", id)
	if err := c.Err(); err != nil {
		return validationErr(err)
	}
	return s.products.Delete(ctx, id)
}
