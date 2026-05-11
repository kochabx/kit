package httpx

import (
	"context"
	"net/http"
)

type Clienter interface {
	Do(ctx context.Context, method, target string, body Body, opts ...RequestOption) (*http.Response, error)
	Get(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error)
	Head(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error)
	Post(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error)
	Put(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error)
	Patch(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error)
	Delete(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error)
}

var _ Clienter = (*Client)(nil)
