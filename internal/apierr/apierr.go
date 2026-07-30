package apierr

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	CodeValidation         = "VALIDATION_ERROR"
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbidden          = "FORBIDDEN"
	CodeHospitalNotFound   = "HOSPITAL_NOT_FOUND"
	CodePatientNotFound    = "PATIENT_NOT_FOUND"
	CodeUsernameTaken      = "USERNAME_TAKEN"
	CodeIdentifierConflict = "PATIENT_IDENTIFIER_CONFLICT"
	CodeHISUnavailable     = "HIS_UNAVAILABLE"
	CodeInternal           = "INTERNAL_ERROR"
)

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

func (e *Error) Unwrap() error { return e.cause }

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

func IdentifierConflict() *Error {
	return newError(http.StatusConflict, CodeIdentifierConflict,
		"the hospital information system returned an identifier that already belongs to a different patient")
}

func HISUnavailable() *Error {
	return newError(http.StatusBadGateway, CodeHISUnavailable, "hospital information system is unavailable")
}

func Internal() *Error {
	return newError(http.StatusInternalServerError, CodeInternal, "internal server error")
}

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
