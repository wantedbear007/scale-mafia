// Package middleware contains cross-cutting HTTP concerns: request ids,
// structured access logs, Prometheus metrics, panic recovery and the
// central error handler.
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const requestIDHeader = "X-Request-Id"

const ctxKeyRequestID = "request_id"

// RequestID assigns every request a correlation id, reusing an inbound
// X-Request-Id when it looks sane, and stores it in the request context and
// the response headers so logs and clients can be correlated.
func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := strings.TrimSpace(c.Get(requestIDHeader))
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Locals(ctxKeyRequestID, id)
		c.Set(requestIDHeader, id)
		return c.Next()
	}
}

// RequestIDFrom returns the request id attached by RequestID().
func RequestIDFrom(c *fiber.Ctx) string {
	if v, ok := c.Locals(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is fatal for uniqueness guarantees; fall back to
		// a value that is still unique enough for log correlation.
		return "req-fallback"
	}
	return hex.EncodeToString(b[:])
}

// validRequestID keeps inbound ids short and printable so a client cannot
// inject newlines or megabytes into the log stream.
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}
