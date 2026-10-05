// Package validation contains hand-rolled input validation.
//
// It is deliberately small and dependency free: the baseline should be simple
// and correct, and validation is not the interesting part of this project.
// The database also enforces CHECK/UNIQUE/FK constraints, so validation here
// exists to return friendly 400s rather than to be the only line of defence.
package validation

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bhanuprataps/scaling-systems/internal/apierr"
)

const (
	MaxNameLength  = 255
	MaxEmailLength = 320
	MaxDescription = 10_000
	MaxPageSize    = 1_000
	MinPageSize    = 1
	MaxPriceDigits = 10
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Error struct {
	Fields []FieldError
}

func (e *Error) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+": "+f.Message)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// ToAPIError converts validation failures into a 400 with a field breakdown.
func (e *Error) ToAPIError() *apierr.Error {
	ae := apierr.BadRequest("validation_error", e.Error())
	ae.WithDetails(e.Fields)
	return ae
}

// Collector accumulates field-level validation failures so a request can
// report every problem at once instead of one per round trip.
type Collector struct {
	fields []FieldError
}

// Add records a field-level failure.
func (c *Collector) Add(field, msg string) {
	c.fields = append(c.fields, FieldError{Field: field, Message: msg})
}

// Err returns nil when no failures were recorded.
func (c *Collector) Err() error {
	if len(c.fields) == 0 {
		return nil
	}
	return &Error{Fields: c.fields}
}

// Fields returns the recorded failures.
func (c *Collector) Fields() []FieldError { return c.fields }

func RequiredString(c *Collector, field, v string, max int) string {
	t := strings.TrimSpace(v)
	if t == "" {
		c.Add(field, "is required and must not be empty")
		return ""
	}
	if utf8.RuneCountInString(t) > max {
		c.Add(field, fmt.Sprintf("must be at most %d characters", max))
	}
	return t
}

func OptionalString(c *Collector, field string, v *string, max int) *string {
	if v == nil {
		return nil
	}
	t := strings.TrimSpace(*v)
	if utf8.RuneCountInString(t) > max {
		c.Add(field, fmt.Sprintf("must be at most %d characters", max))
	}
	return &t
}

func Email(c *Collector, field, v string) string {
	t := strings.ToLower(strings.TrimSpace(v))
	if t == "" {
		c.Add(field, "is required and must not be empty")
		return ""
	}
	if len(t) > MaxEmailLength {
		c.Add(field, fmt.Sprintf("must be at most %d characters", MaxEmailLength))
		return ""
	}
	at := strings.Index(t, "@")
	if at <= 0 || at == len(t)-1 {
		c.Add(field, "must be a valid email address")
		return ""
	}
	if strings.Count(t, "@") != 1 {
		c.Add(field, "must be a valid email address")
		return ""
	}
	domain := t[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		c.Add(field, "must contain a valid domain")
	}
	return t
}

// Money validates a decimal string (NUMERIC(12,2) in PostgreSQL).
func Money(c *Collector, field, v string, required bool) string {
	t := strings.TrimSpace(v)
	if t == "" {
		if required {
			c.Add(field, "is required and must not be empty")
		}
		return ""
	}
	neg := strings.HasPrefix(t, "-")
	body := strings.TrimPrefix(t, "-")

	intPart, fracPart, hasDot := strings.Cut(body, ".")
	if intPart == "" && fracPart == "" {
		c.Add(field, "must be a decimal number")
		return ""
	}
	if !allDigits(intPart) || (hasDot && !allDigits(fracPart)) {
		c.Add(field, "must be a decimal number such as 19.99")
		return ""
	}
	if len(fracPart) > 2 {
		c.Add(field, "must have at most 2 decimal places")
	}
	digits := intPart + fracPart
	if len(strings.TrimLeft(digits, "0")) > MaxPriceDigits {
		c.Add(field, "must be less than 100000000")
	}
	if neg {
		c.Add(field, "must not be negative")
	}
	return t
}

func allDigits(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func Int64ID(c *Collector, field string, v int64) int64 {
	if v <= 0 {
		c.Add(field, "must be a positive integer")
	}
	return v
}

func Int32NonNegative(c *Collector, field string, v int32) int32 {
	if v < 0 {
		c.Add(field, "must not be negative")
	}
	return v
}

func OneOf(c *Collector, field, v string, allowed []string) string {
	for _, a := range allowed {
		if a == v {
			return v
		}
	}
	c.Add(field, "must be one of: "+strings.Join(allowed, ", "))
	return v
}

// PageParams parses ?page=&limit= with baseline defaults (page=1, limit=20).
func PageParams(pageRaw, limitRaw string) (page, limit int, err error) {
	page, limit = 1, 20

	if strings.TrimSpace(pageRaw) != "" {
		n, convErr := strconv.Atoi(strings.TrimSpace(pageRaw))
		if convErr != nil {
			return 0, 0, apierr.BadRequest("invalid_page", "query parameter 'page' must be an integer")
		}
		if n < 1 {
			return 0, 0, apierr.BadRequest("invalid_page", "query parameter 'page' must be >= 1")
		}
		page = n
	}
	if strings.TrimSpace(limitRaw) != "" {
		n, convErr := strconv.Atoi(strings.TrimSpace(limitRaw))
		if convErr != nil {
			return 0, 0, apierr.BadRequest("invalid_limit", "query parameter 'limit' must be an integer")
		}
		if n < MinPageSize {
			return 0, 0, apierr.BadRequest("invalid_limit", fmt.Sprintf("query parameter 'limit' must be >= %d", MinPageSize))
		}
		if n > MaxPageSize {
			return 0, 0, apierr.BadRequest("invalid_limit", fmt.Sprintf("query parameter 'limit' must be <= %d", MaxPageSize))
		}
		limit = n
	}
	return page, limit, nil
}
