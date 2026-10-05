package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// methodOf returns a stable, immutable copy of the HTTP method.
//
// Fiber/fasthttp hands out request strings that point straight into a
// reusable request buffer. Storing such a string in a Prometheus label keeps
// a pointer into memory that fasthttp overwrites on the next request, which
// silently corrupts label values (a "GET" label turning into "GETE"/"PUT").
// Mapping onto compile-time constants avoids that entirely and also bounds
// the method label to the real set of HTTP methods.
func methodOf(c *fiber.Ctx) string {
	switch string(c.Context().Request.Header.Method()) {
	case fiber.MethodGet:
		return "GET"
	case fiber.MethodHead:
		return "HEAD"
	case fiber.MethodPost:
		return "POST"
	case fiber.MethodPut:
		return "PUT"
	case fiber.MethodPatch:
		return "PATCH"
	case fiber.MethodDelete:
		return "DELETE"
	case fiber.MethodConnect:
		return "CONNECT"
	case fiber.MethodOptions:
		return "OPTIONS"
	case fiber.MethodTrace:
		return "TRACE"
	default:
		return "OTHER"
	}
}

// pathOf returns a copy of the request path for log output.
//
// Like methodOf, the value fasthttp returns is only valid until the request
// is recycled, so anything that outlives the handler (log fields, metrics
// labels) must own its own copy.
func pathOf(c *fiber.Ctx) string {
	return string(c.Context().Request.URI().PathOriginal())
}
