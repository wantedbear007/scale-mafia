package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bhanuprataps/scaling-systems/internal/models"
	"github.com/bhanuprataps/scaling-systems/internal/services"
)

type OrderHandler struct {
	svc *services.OrderService
}

func NewOrderHandler(svc *services.OrderService) *OrderHandler { return &OrderHandler{svc: svc} }

func (h *OrderHandler) Create(c *fiber.Ctx) error {
	var req models.CreateOrderRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	o, err := h.svc.Create(c.UserContext(), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(o)
}

func (h *OrderHandler) List(c *fiber.Ctx) error {
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

func (h *OrderHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	o, err := h.svc.GetByID(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(o)
}

func (h *OrderHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req models.UpdateOrderRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	o, err := h.svc.Update(c.UserContext(), id, req)
	if err != nil {
		return err
	}
	return c.JSON(o)
}

func (h *OrderHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
