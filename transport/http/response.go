package http

import (
	"encoding/json"
	stderrors "errors"
	"net/http"

	gv "github.com/go-playground/validator/v10"

	"github.com/kochabx/kit/core/validator"
	"github.com/kochabx/kit/errors"
)

const (
	defaultSuccessMsg = "ok"
	defaultErrorMsg   = "failed"
)

// Response is the standard JSON API response envelope.
type Response[T any] struct {
	Code int    `json:"code"`           // business status code
	Msg  string `json:"msg,omitempty"`  // human-readable message
	Data T      `json:"data,omitempty"` // response payload
}

// OK writes a successful JSON response to w.
// Always returns HTTP 200, code 200, and msg "ok".
func OK[T any](w http.ResponseWriter, data T) {
	if w == nil {
		return
	}
	writeJSON(w, &Response[T]{
		Code: http.StatusOK,
		Msg:  defaultSuccessMsg,
		Data: data,
	})
}

// Fail writes an error JSON response with a custom business code.
// HTTP status is always 200; the business code is carried in the body.
//
// The cause parameter is flexible:
//   - error  → message is extracted from the error (kit/errors.Error preferred)
//   - string → used directly as the message
//   - nil/other → falls back to the default error message
func Fail(w http.ResponseWriter, code int, cause any) {
	if w == nil {
		return
	}

	var message string

	switch v := cause.(type) {
	case error:
		message = errorMessage(v)
	case string:
		message = v
	default:
		message = defaultErrorMsg
	}

	writeJSON(w, &Response[struct{}]{
		Code: code,
		Msg:  message,
	})
}

// writeJSON encodes v as JSON and writes it to w with Content-Type application/json.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// errorMessage returns a human-readable message from err.
func errorMessage(err error) string {
	if err == nil {
		return defaultErrorMsg
	}
	// go-playground/validator or kit validator errors: return err.Error() directly.
	var validationErrors gv.ValidationErrors
	if stderrors.As(err, &validationErrors) || validator.AsValidationError(err) {
		return err.Error()
	}
	var e *errors.Error
	if stderrors.As(err, &e) {
		return e.Message()
	}
	return err.Error()
}
