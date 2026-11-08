package httpx

import "slices"

import "net/http"

// RoundTripFunc adapts a function to http.RoundTripper.
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Middleware intercepts outgoing requests and incoming responses.
//
// Example usage:
//
//	logMW := func(next httpx.RoundTripFunc) httpx.RoundTripFunc {
//	    return func(req *http.Request) (*http.Response, error) {
//	        start := time.Now()
//	        resp, err := next(req)
//	        log.Printf("%s %s -> %v %v", req.Method, req.URL, statusOf(resp), time.Since(start))
//	        return resp, err
//	    }
//	}
type Middleware func(next RoundTripFunc) RoundTripFunc

// chain wraps base in declaration order, with the first middleware outermost.
func chain(base http.RoundTripper, mws []Middleware) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	final := RoundTripFunc(base.RoundTrip)
	// Iterate backward so the first middleware is outermost.
	for _, mw := range slices.Backward(mws) {
		final = mw(final)
	}
	return final
}
