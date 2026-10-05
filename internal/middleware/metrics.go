package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bhanuprataps/scaling-systems/internal/observability"
)

// Metrics records latency, request counts, in-flight gauge and payload sizes.
//
// Labels are method + registered route pattern + status code. Because the
// route pattern comes from the router, the label set is bounded by the number
// of registered routes - raw paths are never used as labels.
func Metrics() fiber.Handler {
	return func(c *fiber.Ctx) error {
		observability.IncInFlight()
		start := time.Now()
		reqBytes := len(c.Body())

		err := c.Next()

		elapsed := time.Since(start)
		status := c.Response().StatusCode()
		if err != nil {
			if e := c.App().ErrorHandler; e != nil {
				_ = e(c, err)
			}
			status = c.Response().StatusCode()
		}

		route := c.Route().Path
		if route == "" {
			route = "unmatched"
		}
		// -1 for "not measurable" (e.g. streamed responses) keeps the
		// histogram from being polluted with zero-byte samples.
		observability.ObserveHTTPRequest(
			methodOf(c), route, status,
			elapsed.Seconds(), reqBytes, len(c.Response().Body()),
		)
		observability.DecInFlight()
		return err
	}
}
