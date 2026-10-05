// Package apierr defines the single error type used across the application
// and helpers for turning errors into consistent JSON HTTP responses.
package apierr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an application error carrying an HTTP status, a stable machine
// readable code and a client-safe message.
type Error struct {
	Status  int
	Code    string
	Message string
	// Wrapped is the underlying cause. It is logged but never returned to the
	// client, so internal details (SQL, hostnames) do not leak.
	Wrapped error
	// Details carries optional structured context (e.g. per-field validation
	// errors). It is safe to expose to clients.
	Details any
}

func (e *Error) WithDetails(d any) *Error {
	e.Details = d
	return e
}

func (e *Error) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Wrapped }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Wrap(status int, code, message string, err error) *Error {
	return &Error{Status: status, Code: code, Message: message, Wrapped: err}
}

func BadRequest(code, message string) *Error { return New(http.StatusBadRequest, code, message) }
func NotFound(code, message string) *Error   { return New(http.StatusNotFound, code, message) }
func Conflict(code, message string) *Error   { return New(http.StatusConflict, code, message) }
func Internal(err error) *Error {
	return Wrap(http.StatusInternalServerError, "internal_error", "an internal error occurred", err)
}

// From normalises any error into an *Error, defaulting to 500.
func From(err error) *Error {
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return Internal(err)
}

// Body is the JSON error envelope returned to clients.
type Body struct {
	Error Payload `json:"error"`
}

type Payload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Details   any    `json:"details,omitempty"`
}
