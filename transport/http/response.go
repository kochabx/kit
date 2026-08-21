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
	defaultOKMsg   = "ok"
	defaultFailMsg = "fail"
)

// Response defines the JSON envelope returned by HTTP handlers.
// Code is an application-level business code and is independent of the HTTP status.
type Response[T any] struct {
	Code int    `json:"code"`           // application-level business code
	Msg  string `json:"msg,omitempty"`  // message intended for the client
	Data T      `json:"data,omitempty"` // successful response payload
}

// OK writes a successful response with HTTP status 200, business code 200,
// message "ok", and the supplied payload. A nil writer is ignored.
func OK[T any](w http.ResponseWriter, data T) {
	if w == nil {
		return
	}
	writeJSON(w, http.StatusOK, &Response[T]{
		Code: http.StatusOK,
		Msg:  defaultOKMsg,
		Data: data,
	})
}

// Fail writes an error response using status as the HTTP status and bcode as
// the application-level business code.
//
// The response message is selected from cause as follows:
//   - error: use the error message; errors.Error uses its client-facing Message
//   - string: use the string unchanged
//   - any other value, including nil: use "fail"
func Fail(w http.ResponseWriter, status, bcode int, cause any) {
	if w == nil {
		return
	}

	var msg string

	switch v := cause.(type) {
	case error:
		msg = message(v)
	case string:
		msg = v
	default:
		msg = defaultFailMsg
	}

	writeJSON(w, status, &Response[any]{
		Code: bcode,
		Msg:  msg,
	})
}

// writeJSON marshals v before committing the response headers. Invalid HTTP
// statuses and JSON encoding failures produce a generic HTTP 500 response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	status = normalizeStatus(status)
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body = fallbackBody()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func fallbackBody() []byte {
	body, _ := json.Marshal(&Response[any]{
		Code: http.StatusInternalServerError,
		Msg:  defaultFailMsg,
	})
	return body
}

func normalizeStatus(status int) int {
	if status < 100 || status > 599 {
		return http.StatusInternalServerError
	}
	return status
}

// message extracts the client-facing message from err. Validation errors keep
// their formatted details, errors.Error uses Message, and other errors use Error.
func message(err error) string {
	if err == nil {
		return defaultFailMsg
	}
	var ve gv.ValidationErrors
	if stderrors.As(err, &ve) || validator.AsError(err) {
		return err.Error()
	}
	if e, ok := stderrors.AsType[*errors.Error](err); ok {
		return e.Message()
	}
	return err.Error()
}
