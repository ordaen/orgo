package handlers

import (
	"errors"
	"strconv"
	"strings"

	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/validator"
)

var (
	// ErrorRecordNotFound is the error of a missing record.
	ErrorRecordNotFound = Error{
		Status: "404",
		Title:  "Record Not Found",
		Detail: "The record you are looking for does not exist",
	}
	// ErrorPageNotFound is the error of a missing page.
	ErrorPageNotFound = Error{
		Status: "404",
		Title:  "Page Not Found",
		Detail: "The page you are looking for does not exist",
	}
	// ErrorUnauthorized is the error of a request without a valid session.
	ErrorUnauthorized = Error{
		Status: "401",
		Title:  "Unauthorized Request",
		Detail: "You are forbidden from accessing this page",
	}
)

// Error is an error of a response, with the HTTP status of the response.
type Error struct {
	Code      string `json:"code,omitempty"`
	Status    string `json:"status,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Title     string `json:"title,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Error returns the detail.
func (e Error) Error() string {
	return e.Detail
}

// StatusCode returns the HTTP status, 500 when Status is not a client or server error status.
func (e Error) StatusCode() int {
	code, err := strconv.Atoi(e.Status)
	if err != nil || code < 400 || code > 599 {
		return 500
	}
	return code
}

// ErrorInternal returns a 500 error with the details.
func ErrorInternal(details string) Error {
	return Error{Status: "500", Title: "Internal Server Error", Detail: details}
}

// ErrorInvalidAttribute returns a 422 error of the attribute with the details.
func ErrorInvalidAttribute(attribute, details string) Error {
	return Error{Status: "422", Attribute: attribute, Title: "Invalid Attribute", Detail: details}
}

// ErrorBadRequest returns a 400 error with the details.
func ErrorBadRequest(details string) Error {
	return Error{Status: "400", Title: "Bad Request", Detail: details}
}

// ErrorForbidden returns a 403 error with the details.
func ErrorForbidden(details string) Error {
	return Error{Status: "403", Title: "Forbidden", Detail: details}
}

// Errors are the errors of a response.
type Errors struct {
	Errors []Error `json:"errors,omitempty"`
}

// HasErrors reports whether there are errors.
func (e Errors) HasErrors() bool {
	return len(e.Errors) > 0
}

// AddError adds err, nothing when it is nil. An Error or Errors is added as is, a *validator.Error as 422 errors,
// pg.ErrRecordNotFound as ErrorRecordNotFound and the other errors as ErrorInternal with their message.
func (e *Errors) AddError(err error) {
	if err == nil {
		return
	}
	var (
		single Error
		multi  Errors
		valid  *validator.Error
	)
	switch {
	case errors.As(err, &single):
		e.Errors = append(e.Errors, single)
	case errors.As(err, &multi):
		e.Errors = append(e.Errors, multi.Errors...)
	case errors.As(err, &valid):
		for _, msg := range valid.Errors {
			e.Errors = append(e.Errors, Error{Status: "422", Title: "Invalid Attribute", Detail: msg})
		}
	case errors.Is(err, pg.ErrRecordNotFound):
		e.Errors = append(e.Errors, ErrorRecordNotFound)
	default:
		e.Errors = append(e.Errors, ErrorInternal(err.Error()))
	}
}

// Error returns the details of the errors separated by commas.
func (e Errors) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, v := range e.Errors {
		msgs = append(msgs, v.Detail)
	}
	return strings.Join(msgs, ",")
}

// StatusCode returns the status of the first error, 200 without errors.
func (e Errors) StatusCode() int {
	if len(e.Errors) == 0 {
		return 200
	}
	return e.Errors[0].StatusCode()
}
