package middleware

import (
	"errors"
	"log/slog"
	"runtime/debug"

	"github.com/gofiber/fiber/v2"

	"github.com/bhanuprataps/scaling-systems/internal/apierr"
)

// ErrorHandler is the application-wide error handler. It converts any error
// returned by a handler into a consistent JSON envelope and never leaks
// internal details (SQL text, hostnames, stack traces) to clients.
func ErrorHandler(log *slog.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		appErr := apierr.From(err)

		// Fiber's own 404 (fiber.ErrNotFound) arrives here for unmatched routes.
		var fe *fiber.Error
		if errors.As(err, &fe) {
			appErr = apierr.New(fe.Code, httpCodeName(fe.Code), fe.Message)
		}

		// 4xx is already logged once, at WARN, by the access-log middleware.
		// Logging it here again would double every client-error line, so this
		// handler only adds detail for genuine server-side failures.
		if appErr.Status >= fiber.StatusInternalServerError {
			log.Error("request_failed",
				slog.String("request_id", RequestIDFrom(c)),
				slog.String("method", methodOf(c)),
				slog.String("path", pathOf(c)),
				slog.Int("status", appErr.Status),
				slog.String("code", appErr.Code),
				slog.String("error", err.Error()),
			)
		} else {
			log.Debug("request_rejected",
				slog.String("request_id", RequestIDFrom(c)),
				slog.String("method", methodOf(c)),
				slog.String("path", pathOf(c)),
				slog.Int("status", appErr.Status),
				slog.String("code", appErr.Code),
			)
		}

		return c.Status(appErr.Status).JSON(apierr.Body{
			Error: apierr.Payload{
				Code:      appErr.Code,
				Message:   appErr.Message,
				RequestID: RequestIDFrom(c),
				Details:   appErr.Details,
			},
		})
	}
}

// Recoverer turns a panic into a 500 with a logged stack trace instead of
// taking the process down.
func Recoverer(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic_recovered",
					slog.String("request_id", RequestIDFrom(c)),
					slog.String("method", methodOf(c)),
					slog.String("path", pathOf(c)),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				err = apierr.Internal(nil)
			}
		}()
		return c.Next()
	}
}

func httpCodeName(code int) string {
	switch code {
	case fiber.StatusBadRequest:
		return "bad_request"
	case fiber.StatusUnauthorized:
		return "unauthorized"
	case fiber.StatusForbidden:
		return "forbidden"
	case fiber.StatusNotFound:
		return "not_found"
	case fiber.StatusMethodNotAllowed:
		return "method_not_allowed"
	case fiber.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case fiber.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case fiber.StatusTooManyRequests:
		return "too_many_requests"
	case fiber.StatusInternalServerError:
		return "internal_error"
	case fiber.StatusServiceUnavailable:
		return "service_unavailable"
	case fiber.StatusGatewayTimeout:
		return "gateway_timeout"
	default:
		return "http_error"
	}
}
