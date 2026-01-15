package http

import "net/http"

// Clienter defines the interface for HTTP client operations
type Clienter interface {
	Request(method, url string, body any, opts ...func(*RequestOption)) (*http.Response, error)
}
