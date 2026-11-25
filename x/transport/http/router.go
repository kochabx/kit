package http

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"reflect"

	"github.com/kochabx/kit/fault"
)

// Validator checks decoded request payloads (directly satisfied by x/validate.Validator).
type Validator interface {
	Struct(ctx context.Context, v any) error
}

// ValidatorFunc adapts a function to Validator.
type ValidatorFunc func(context.Context, any) error

func (f ValidatorFunc) Struct(ctx context.Context, v any) error { return f(ctx, v) }

// RouterOption customizes Router construction.
type RouterOption func(*Router)

// WithMaxBody limits the maximum decoded request body bytes (default 1 MiB).
func WithMaxBody(limit int64) RouterOption {
	return func(rt *Router) {
		if limit > 0 {
			rt.maxBody = limit
		}
	}
}

// WithValidator attaches payload validation invoked before handler execution.
func WithValidator(v Validator) RouterOption {
	return func(rt *Router) { rt.validator = v }
}

// Router wraps http.ServeMux with typed JSON handlers.
type Router struct {
	mux       *http.ServeMux
	validator Validator
	maxBody   int64
}

// NewRouter constructs a Router with sensible body size defaults.
func NewRouter(opts ...RouterOption) *Router {
	rt := &Router{
		mux:     http.NewServeMux(),
		maxBody: 1 << 20,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(rt)
		}
	}
	return rt
}

// ServeHTTP dispatches requests through the underlying ServeMux.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mux.ServeHTTP(w, r)
}

// HandleRaw registers a standard http.Handler for pattern.
func (rt *Router) HandleRaw(pattern string, h http.Handler) {
	rt.mux.Handle(pattern, h)
}

// HandleFunc registers a standard http.HandlerFunc for pattern.
func (rt *Router) HandleFunc(pattern string, fn http.HandlerFunc) {
	rt.mux.HandleFunc(pattern, fn)
}

var emptyStructType = reflect.TypeFor[struct{}]()

// Handle registers a typed route that decodes JSON request bodies, validates inputs, and renders Envelope/Problem outputs.
func (rt *Router) Handle[Req, Resp any](
	pattern string,
	fn func(ctx context.Context, req Req) (Resp, error),
) {
	isEmptyReq := reflect.TypeFor[Req]() == emptyStructType
	rt.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if !isEmptyReq && r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
			limited := http.MaxBytesReader(w, r.Body, rt.maxBody)
			if err := json.UnmarshalRead(limited, &req); err != nil {
				if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
					Fail(w, r, fault.BadRequest("request body too large").WithStatus(http.StatusRequestEntityTooLarge).WithCause(err))
					return
				}
				if !errors.Is(err, io.EOF) {
					Fail(w, r, fault.InvalidArgument("invalid request body").WithCause(err))
					return
				}
			}
		}
		if !isEmptyReq && rt.validator != nil {
			if err := rt.validator.Struct(r.Context(), &req); err != nil {
				if _, isFault := fault.From(err); isFault {
					Fail(w, r, err)
				} else {
					Fail(w, r, fault.InvalidArgument("validation failed").WithCause(err))
				}
				return
			}
		}
		resp, err := fn(r.Context(), req)
		if err != nil {
			Fail(w, r, err)
			return
		}
		OK(w, resp)
	})
}
