// Package handlers contains the HTTP layer. Handlers parse/validate the
// request, call a service, and shape the response. No SQL lives here.
package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/bhanuprataps/scaling-systems/internal/apierr"
	"github.com/bhanuprataps/scaling-systems/internal/validation"
)

// parseID reads the :id path parameter.
func parseID(c *fiber.Ctx) (int64, error) {
	raw := c.Params("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, apierr.BadRequest("invalid_id", "path parameter 'id' must be a positive integer")
	}
	return id, nil
}

// parsePage reads ?page=&limit= with baseline defaults page=1, limit=20.
func parsePage(c *fiber.Ctx) (page, limit int, err error) {
	return validation.PageParams(c.Query("page"), c.Query("limit"))
}

// parseBody decodes a JSON request body, mapping Fiber's errors onto a
// consistent 400.
func parseBody(c *fiber.Ctx, dst any) error {
	if len(c.Body()) == 0 {
		return apierr.BadRequest("empty_body", "a JSON request body is required")
	}
	if err := c.BodyParser(dst); err != nil {
		return apierr.BadRequest("invalid_json", "request body must be valid JSON matching the endpoint schema")
	}
	return nil
}
