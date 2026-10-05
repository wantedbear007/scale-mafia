package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Logger emits one structured line per request.
//
// It deliberately logs metadata only - never request or response bodies, never
// headers that can carry credentials. Fields: time, level, method, route,
// path, status, duration, bytes, ip, user agent and request id.
func Logger(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		elapsed := time.Since(start)

		status := c.Response().StatusCode()
		if err != nil {
			// The error handler has not run yet; approximate what it will do.
			if e := c.App().ErrorHandler; e != nil {
				_ = e(c, err)
			}
			status = c.Response().StatusCode()
		}

		// c.Route().Path is the registered pattern ("/api/v1/users/:id"),
		// which keeps log fields and metrics labels low cardinality.
		route := c.Route().Path
		if route == "" {
			route = "unmatched"
		}

		attrs := []any{
			slog.String("method", methodOf(c)),
			slog.String("route", route),
			slog.String("path", pathOf(c)),
			slog.Int("status", status),
			slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000.0),
			slog.Int("bytes", len(c.Response().Body())),
			slog.String("ip", string(c.IP())),
			slog.String("user_agent", truncate(string(c.Get(fiber.HeaderUserAgent)), 128)),
			slog.String("request_id", RequestIDFrom(c)),
		}

		switch {
		case status >= fiber.StatusInternalServerError:
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}
			log.Error("request", attrs...)
		case status >= fiber.StatusBadRequest:
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}
			log.Warn("request", attrs...)
		default:
			log.Info("request", attrs...)
		}
		return err
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
