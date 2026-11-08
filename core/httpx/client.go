package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client wraps an HTTP client and provides:
//   - Content-Type inferred from the body type
//   - A base URL, default headers, and timeouts
//   - A RoundTripper middleware chain
//   - Status-based HTTPError responses
//   - Optional retries and backoff
//   - Response decoding through Into, IntoJSON, IntoXML, IntoBytes, and IntoString
//
// Client is safe for concurrent use after configuration.
type Client struct {
	httpClient    *http.Client
	transport     http.RoundTripper // The underlying RoundTripper supplied through WithTransport.
	baseURL       string
	defaultHeader http.Header
	middlewares   []Middleware
	errorOnStatus func(int) bool
	retry         retryConfig
}

// retryConfig controls retries. MaxAttempts <= 1 disables retries.
type retryConfig struct {
	MaxAttempts int
	Backoff     BackoffFunc
	RetryOn     func(resp *http.Response, err error) bool
}

// BackoffFunc returns the delay after a failed attempt, numbered from one.
type BackoffFunc func(attempt int) time.Duration

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient supplies a custom *http.Client.
//
// WithTransport, WithTimeout, and middleware options also apply to this client,
// replacing its Transport or Timeout as configured.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cli *Client) {
		if c != nil {
			cli.httpClient = c
		}
	}
}

// WithTransport sets the innermost RoundTripper beneath the middleware chain.
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(cli *Client) { cli.transport = rt }
}

// WithTimeout sets the request timeout, including response body reads.
func WithTimeout(d time.Duration) ClientOption {
	return func(cli *Client) { cli.httpClient.Timeout = d }
}

// WithBaseURL sets the base URL used to resolve relative request targets.
func WithBaseURL(base string) ClientOption {
	return func(cli *Client) { cli.baseURL = strings.TrimRight(base, "/") }
}

// WithDefaultHeader sets a default header; request-specific headers override matching names.
func WithDefaultHeader(key, value string) ClientOption {
	return func(cli *Client) { cli.defaultHeader.Set(key, value) }
}

// WithMiddleware adds middleware in order, with the first one outermost.
func WithMiddleware(mw ...Middleware) ClientOption {
	return func(cli *Client) { cli.middlewares = append(cli.middlewares, mw...) }
}

// WithErrorOnStatus defines which status codes produce errors; the default is code >= 400.
// A nil function disables status-based errors for all responses.
func WithErrorOnStatus(fn func(int) bool) ClientOption {
	return func(cli *Client) { cli.errorOnStatus = fn }
}

// WithRetry enables retries. maxAttempts includes the initial request.
// A nil backoff disables waiting. retryOn selects retryable results; nil uses the default
// policy for network errors, 5xx responses, and status 429.
//
// Retries require a replayable body. The provided JSON, XML, Form, Raw, Text, and ReadAll
// bodies buffer their data during encoding so retries can replay it.
func WithRetry(maxAttempts int, backoff BackoffFunc, retryOn func(*http.Response, error) bool) ClientOption {
	return func(cli *Client) {
		cli.retry = retryConfig{
			MaxAttempts: maxAttempts,
			Backoff:     backoff,
			RetryOn:     retryOn,
		}
	}
}

// ExpBackoff returns an exponential backoff sequence: base, 2*base, 4*base, and so on.
func ExpBackoff(base time.Duration) BackoffFunc {
	return func(attempt int) time.Duration {
		if attempt < 1 {
			attempt = 1
		}
		return base << (attempt - 1)
	}
}

// New creates a Client.
func New(opts ...ClientOption) *Client {
	c := &Client{
		httpClient:    &http.Client{},
		defaultHeader: make(http.Header),
		errorOnStatus: defaultErrorOnStatus,
	}
	for _, opt := range opts {
		opt(c)
	}
	// Assemble the transport and middleware chain.
	c.httpClient.Transport = chain(c.transport, c.middlewares)
	return c
}

// defaultErrorOnStatus selects 4xx and 5xx responses.
func defaultErrorOnStatus(code int) bool {
	return code >= 400
}

// defaultRetryOn selects network errors, 5xx responses, and status 429.
func defaultRetryOn(resp *http.Response, err error) bool {
	if err != nil {
		// Do not retry context errors.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}
	if resp == nil {
		return false
	}
	return resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
}

// Do executes an HTTP request.
//
// target may be an absolute URL or a relative path resolved against WithBaseURL.
// A nil body sends no payload; otherwise use a Body constructor such as JSON or Form.
func (c *Client) Do(ctx context.Context, method, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("nil Context")
	}

	cfg := &requestConfig{
		header: make(http.Header),
		query:  make(url.Values),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// 1. Resolve the URL.
	fullURL, err := c.resolveURL(target, cfg.query)
	if err != nil {
		return nil, err
	}

	// 2. Encode the body.
	var bodyBytes []byte
	var bodyContentType string
	if body != nil {
		bodyBytes, bodyContentType, err = body.Encode()
		if err != nil {
			return nil, err
		}
	}

	// 3. Merge headers: defaults, inferred body headers, then explicit request headers.
	header := make(http.Header, len(c.defaultHeader)+len(cfg.header)+1)
	for k, vs := range c.defaultHeader {
		header[k] = append([]string(nil), vs...)
	}
	if bodyContentType != "" && header.Get("Content-Type") == "" {
		header.Set("Content-Type", bodyContentType)
	}
	for k, vs := range cfg.header {
		header[k] = append([]string(nil), vs...)
	}

	// 4. Execute the request, including retries.
	resp, err := c.doWithRetry(ctx, method, fullURL, header, bodyBytes)
	if err != nil {
		return nil, err
	}

	// 5. Convert configured error statuses into errors.
	if c.errorOnStatus != nil && c.errorOnStatus(resp.StatusCode) {
		return resp, newHTTPError(method, fullURL, resp)
	}

	// 6. Decode the response.
	if cfg.decode != nil {
		if err := cfg.decode(resp); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

// resolveURL combines the base URL, target, and query parameters.
func (c *Client) resolveURL(target string, extraQuery url.Values) (string, error) {
	var full string
	switch {
	case c.baseURL == "":
		full = target
	case target == "":
		full = c.baseURL
	case strings.Contains(target, "://"):
		full = target
	default:
		if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		full = c.baseURL + target
	}

	if len(extraQuery) == 0 {
		return full, nil
	}
	u, err := url.Parse(full)
	if err != nil {
		return "", fmt.Errorf("httpx: parse url %q: %w", full, err)
	}
	q := u.Query()
	for k, vs := range extraQuery {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// doWithRetry rebuilds the request for each attempt so the body can be replayed.
func (c *Client) doWithRetry(ctx context.Context, method, fullURL string, header http.Header, bodyBytes []byte) (*http.Response, error) {
	maxAttempts := max(c.retry.MaxAttempts, 1)
	retryOn := c.retry.RetryOn
	if retryOn == nil {
		retryOn = defaultRetryOn
	}

	var lastResp *http.Response
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := c.buildRequest(ctx, method, fullURL, header, bodyBytes)
		if err != nil {
			return nil, err
		}
		lastResp, lastErr = c.httpClient.Do(req)
		if attempt == maxAttempts || !retryOn(lastResp, lastErr) {
			break
		}
		// Drain and close failed responses so their connections can be reused.
		if lastResp != nil {
			_, _ = io.Copy(io.Discard, lastResp.Body)
			_ = lastResp.Body.Close()
			lastResp = nil
		}
		// Wait for backoff.
		if c.retry.Backoff != nil {
			select {
			case <-time.After(c.retry.Backoff(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return lastResp, lastErr
}

// buildRequest constructs one HTTP request.
func (c *Client) buildRequest(ctx context.Context, method, fullURL string, header http.Header, bodyBytes []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("httpx: build request: %w", err)
	}
	req.Header = header.Clone()
	if bodyBytes != nil {
		// Provide GetBody so the standard library can replay requests, such as on 307/308 redirects.
		req.ContentLength = int64(len(bodyBytes))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
	return req, nil
}

// newHTTPError builds an HTTPError and consumes the response body, retaining a bounded snapshot.
func newHTTPError(method, fullURL string, resp *http.Response) *HTTPError {
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, ErrorBodyLimit)
	data, _ := io.ReadAll(limited)
	// Drain remaining bytes to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)
	return &HTTPError{
		Method:     method,
		URL:        fullURL,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       data,
	}
}

// Get sends a GET request.
func (c *Client) Get(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodGet, target, nil, opts...)
}

// Head sends a HEAD request.
func (c *Client) Head(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodHead, target, nil, opts...)
}

// Post sends a POST request.
func (c *Client) Post(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodPost, target, body, opts...)
}

// Put sends a PUT request.
func (c *Client) Put(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodPut, target, body, opts...)
}

// Patch sends a PATCH request.
func (c *Client) Patch(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodPatch, target, body, opts...)
}

// Delete sends a DELETE request.
func (c *Client) Delete(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodDelete, target, nil, opts...)
}
