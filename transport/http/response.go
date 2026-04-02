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
// Always returns HTTP 200, code 200, and msg "success".
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
// The msg parameter is flexible:
//   - error  → message is extracted from the error (kit/errors.Error preferred)
//   - string → used directly as the message
//   - nil    → falls back to the default error message
//   - other  → placed in the Data field
func Fail(w http.ResponseWriter, code int, msg any) {
	if w == nil {
		return
	}

	var message string
	var data any

	switch v := msg.(type) {
	case error:
		message = extractErrorMessage(v)
	case string:
		message = v
	case nil:
		message = defaultErrorMsg
	default:
		data = v
	}

	writeJSON(w, &Response[any]{
		Code: code,
		Msg:  message,
		Data: data,
	})
}

// writeJSON encodes v as JSON and writes it to w with Content-Type application/json.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// extractErrorMessage returns a human-readable message from err.
// It prefers the structured message from kit/errors.Error over err.Error().
func extractErrorMessage(err error) string {
	if err == nil {
		return defaultErrorMsg
	}
	// go-playground/validator or kit validator errors: return err.Error() directly.
	var gvErrs gv.ValidationErrors
	if stderrors.As(err, &gvErrs) || validator.AsValidationError(err) {
		return err.Error()
	}
	if e := errors.FromError(err); e != nil {
		return e.Message
	}
	return err.Error()
}
