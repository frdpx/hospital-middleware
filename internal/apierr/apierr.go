// Package apierr defines the single error type that travels from repository
// and service layers up to the HTTP handlers, so that every endpoint renders
// the same error envelope without each handler re-deciding status codes.
package apierr

import (
	"errors"
	"fmt"
	"net/http"
)

// Machine-readable error codes returned in the response body. Clients should
// branch on these, never on the human-readable message.
const (
	CodeValidation         = "VALIDATION_ERROR"
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbidden          = "FORBIDDEN"
	CodeHospitalNotFound   = "HOSPITAL_NOT_FOUND"
	CodePatientNotFound    = "PATIENT_NOT_FOUND"
	CodeUsernameTaken      = "USERNAME_TAKEN"
	CodeHISUnavailable     = "HIS_UNAVAILABLE"
	CodeInternal           = "INTERNAL_ERROR"
)

// Error is an HTTP-aware error: it carries the status code and the client-safe
// message, while keeping the underlying cause for logging only.
type Error struct {
	Status  int
	Code    string
	Message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause to errors.Is/errors.As without ever putting it in a
// response body — internal details (SQL text, upstream URLs) must not leak.
func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches an underlying error for logging. It returns a copy so the
// package-level sentinel values stay immutable.
func (e *Error) WithCause(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

func newError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Validation(message string) *Error {
	return newError(http.StatusBadRequest, CodeValidation, message)
}

func InvalidCredentials() *Error {
	// Deliberately vague: never reveal whether the username or the hospital
	// existed, which would let an attacker enumerate accounts.
	return newError(http.StatusUnauthorized, CodeInvalidCredentials, "username, password or hospital is incorrect")
}

func Unauthorized(message string) *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, message)
}

func Forbidden(message string) *Error {
	return newError(http.StatusForbidden, CodeForbidden, message)
}

func HospitalNotFound() *Error {
	return newError(http.StatusNotFound, CodeHospitalNotFound, "hospital does not exist")
}

func PatientNotFound() *Error {
	return newError(http.StatusNotFound, CodePatientNotFound, "no patient matches the given criteria")
}

func UsernameTaken() *Error {
	return newError(http.StatusConflict, CodeUsernameTaken, "username already exists for this hospital")
}

func HISUnavailable() *Error {
	return newError(http.StatusBadGateway, CodeHISUnavailable, "hospital information system is unavailable")
}

func Internal() *Error {
	return newError(http.StatusInternalServerError, CodeInternal, "internal server error")
}

// From maps any error to an *Error. Errors that were never classified become a
// generic 500 so that unexpected internals are never rendered to a client.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return Internal().WithCause(err)
}
