package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bhanuprataps/scaling-systems/internal/models"
	"github.com/bhanuprataps/scaling-systems/internal/services"
)

type UserHandler struct {
	svc *services.UserService
}

func NewUserHandler(svc *services.UserService) *UserHandler { return &UserHandler{svc: svc} }

func (h *UserHandler) Create(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	u, err := h.svc.Create(c.UserContext(), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(u)
}

func (h *UserHandler) List(c *fiber.Ctx) error {
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

func (h *UserHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	u, err := h.svc.GetByID(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(u)
}

func (h *UserHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req models.UpdateUserRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	u, err := h.svc.Update(c.UserContext(), id, req)
	if err != nil {
		return err
	}
	return c.JSON(u)
}

func (h *UserHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
