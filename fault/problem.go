package fault

import (
	"cmp"
	"net/http"
)

// Problem represents an RFC 9457 Problem Details payload.
type Problem struct {
	Type       string            `json:"type,omitzero"`
	Title      string            `json:"title,omitzero"`
	Status     int               `json:"status"`
	Detail     string            `json:"detail,omitzero"`
	Instance   string            `json:"instance,omitzero"`
	Code       Code              `json:"code,omitzero"`
	Extensions map[string]string `json:"extensions,omitzero"`
}

// Problem converts the Error into an RFC 9457 Problem Details value.
// Underlying causes are intentionally omitted to avoid leaking internal state.
func (e *Error) Problem(instance string) Problem {
	if e == nil {
		return Problem{
			Type:   "about:blank",
			Title:  http.StatusText(http.StatusInternalServerError),
			Status: http.StatusInternalServerError,
			Code:   CodeInternal,
		}
	}
	status := e.HTTPStatus()
	return Problem{
		Type:       "about:blank",
		Title:      cmp.Or(http.StatusText(status), "Error"),
		Status:     status,
		Detail:     e.message,
		Instance:   instance,
		Code:       e.code,
		Extensions: e.Metadata(),
	}
}
