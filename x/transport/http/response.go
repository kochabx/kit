// Package http provides an HTTP server lifecycle, typed JSON routing, RFC 9457 Problem
// responses, built-in Scalar OpenAPI UI, and security middlewares.
package http

import (
	"encoding/json/v2"
	"net/http"

	"github.com/kochabx/kit/cx"
	"github.com/kochabx/kit/fault"
)

// Envelope wraps successful typed JSON responses, preserving empty slices via omitzero.
type Envelope[T any] struct {
	Code    string `json:"code"`
	Message string `json:"message,omitzero"`
	Data    T      `json:"data,omitzero"`
}

const fallbackInternalProblem = `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"INTERNAL"}`

func writeInternalProblem(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(fallbackInternalProblem))
}

// JSON serializes v as JSON with nosniff headers.
func JSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeInternalProblem(w)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// OK writes an Envelope[T] with HTTP 200 OK.
func OK[T any](w http.ResponseWriter, data T) {
	JSON(w, http.StatusOK, Envelope[T]{Code: "OK", Data: data})
}

// Fail writes an RFC 9457 Problem Details payload derived from err.
// Non-fault errors are masked as generic internal errors to avoid leaking internals.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	f, ok := fault.From(err)
	if !ok {
		f = fault.Internal("internal server error")
	}
	instance := ""
	if r != nil && r.URL != nil {
		instance = r.URL.Path
	}
	prob := f.Problem(instance)
	body, mErr := json.Marshal(prob)
	if mErr != nil {
		writeInternalProblem(w)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(f.HTTPStatus())
	_, _ = w.Write(body)
}

type componentHealthResponse struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Error  string `json:"error,omitzero"`
}

type healthResponse struct {
	Healthy    bool                      `json:"healthy"`
	Components []componentHealthResponse `json:"components,omitzero"`
}

// HealthHandler serves *cx.Container health checks as HTTP 200 or 503 JSON.
func HealthHandler(c *cx.Container) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c == nil {
			JSON(w, http.StatusOK, healthResponse{Healthy: true})
			return
		}
		report, err := c.HealthCheck(r.Context())
		if err != nil {
			Fail(w, r, fault.ServiceUnavailable("health check unavailable").WithCause(err))
			return
		}
		components := make([]componentHealthResponse, 0, len(report.Components))
		for _, comp := range report.Components {
			item := componentHealthResponse{
				Key:    comp.Key,
				Status: comp.Status.String(),
			}
			if comp.Error != nil {
				item.Error = comp.Error.Error()
			}
			components = append(components, item)
		}
		status := http.StatusOK
		if !report.Healthy {
			status = http.StatusServiceUnavailable
		}
		JSON(w, status, healthResponse{
			Healthy:    report.Healthy,
			Components: components,
		})
	}
}
