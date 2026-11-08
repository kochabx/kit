package httpx

import (
	"fmt"
	"net/http"
)

// HTTPError represents a completed HTTP exchange whose status is configured as an error.
//
// WithErrorOnStatus controls this policy; by default, codes >= 400 produce errors.
type HTTPError struct {
	Method     string      // Request method.
	URL        string      // Final request URL.
	StatusCode int         // HTTP status code.
	Status     string      // Status line, such as "404 Not Found".
	Header     http.Header // Response headers.
	Body       []byte      // Response body snapshot, bounded by ErrorBodyLimit.
}

// ErrorBodyLimit bounds the response body snapshot retained in an HTTPError.
const ErrorBodyLimit = 4 * 1024

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if len(e.Body) == 0 {
		return fmt.Sprintf("httpx: %s %s -> %s", e.Method, e.URL, e.Status)
	}
	return fmt.Sprintf("httpx: %s %s -> %s: %s", e.Method, e.URL, e.Status, e.Body)
}

// Is allows errors.Is to match by StatusCode.
//
//	errors.Is(err, &HTTPError{StatusCode: 404})
func (e *HTTPError) Is(target error) bool {
	t, ok := target.(*HTTPError)
	if !ok {
		return false
	}
	return t.StatusCode == e.StatusCode
}
