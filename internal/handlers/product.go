package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bhanuprataps/scaling-systems/internal/models"
	"github.com/bhanuprataps/scaling-systems/internal/services"
)

type ProductHandler struct {
	svc *services.ProductService
}

func NewProductHandler(svc *services.ProductService) *ProductHandler {
	return &ProductHandler{svc: svc}
}

func (h *ProductHandler) Create(c *fiber.Ctx) error {
	var req models.CreateProductRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	p, err := h.svc.Create(c.UserContext(), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(p)
}

func (h *ProductHandler) List(c *fiber.Ctx) error {
	page, limit, err := parsePage(c)
	if err != nil {
		return err
	}
	coll, err := h.svc.List(c.UserContext(), page, limit)
	if err != nil {
		return err
	}
	return c.JSON(coll)
}

func (h *ProductHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	p, err := h.svc.GetByID(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(p)
}

func (h *ProductHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req models.UpdateProductRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	p, err := h.svc.Update(c.UserContext(), id, req)
	if err != nil {
		return err
	}
	return c.JSON(p)
}

func (h *ProductHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
