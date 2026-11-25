// Package http provides a typed REST client built on resty.dev/v3, otelhttp, and *fault.Error mapping.
package http

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"resty.dev/v3"

	"github.com/kochabx/kit/fault"
)

type options struct {
	baseURL       string
	timeout       time.Duration
	retryCount    int
	retryWaitTime time.Duration
	retryMaxWait  time.Duration
	headers       map[string]string
	bearerToken   string
	tlsConfig     *tls.Config
	otelEnabled   bool
	logger        *slog.Logger
}

// Option customizes Client construction.
type Option func(*options)

func WithBaseURL(baseURL string) Option {
	return func(o *options) { o.baseURL = baseURL }
}

func WithTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.timeout = timeout
		}
	}
}

func WithRetry(count int, waitTime, maxWait time.Duration) Option {
	return func(o *options) {
		if count >= 0 {
			o.retryCount = count
		}
		if waitTime > 0 {
			o.retryWaitTime = waitTime
		}
		if maxWait > 0 {
			o.retryMaxWait = maxWait
		}
	}
}

func WithHeader(k, v string) Option {
	return func(o *options) {
		if k != "" {
			o.headers[k] = v
		}
	}
}

func WithBearerToken(token string) Option {
	return func(o *options) { o.bearerToken = token }
}

func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(o *options) {
		if tlsCfg != nil {
			o.tlsConfig = tlsCfg.Clone()
		}
	}
}

func WithOTel(enabled bool) Option {
	return func(o *options) { o.otelEnabled = enabled }
}

func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// RequestOption configures a single outgoing HTTP call.
type RequestOption func(*resty.Request)

func WithQuery(key, value string) RequestOption {
	return func(r *resty.Request) { r.SetQueryParam(key, value) }
}

func WithRequestHeader(key, value string) RequestOption {
	return func(r *resty.Request) { r.SetHeader(key, value) }
}

// Client wraps *resty.Client with typed JSON helpers and fault error mapping.
type Client struct {
	baseURL   string
	resty     *resty.Client
	transport *http.Transport
	logger    *slog.Logger
}

// New constructs a Client configured with OTel transport, retries, and cloned TLS state.
func New(opts ...Option) (*Client, error) {
	o := options{
		timeout:       15 * time.Second,
		retryWaitTime: 100 * time.Millisecond,
		retryMaxWait:  2 * time.Second,
		headers:       make(map[string]string),
		otelEnabled:   true,
		logger:        slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	var (
		transport *http.Transport
		rt        http.RoundTripper
	)
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		transport = base.Clone()
		if o.tlsConfig != nil {
			transport.TLSClientConfig = o.tlsConfig
		}
		rt = transport
	} else if o.tlsConfig != nil {
		return nil, fmt.Errorf("http client: cannot apply TLSConfig to non-*http.Transport DefaultTransport (%T)", http.DefaultTransport)
	} else if http.DefaultTransport != nil {
		rt = http.DefaultTransport
	} else {
		transport = &http.Transport{}
		rt = transport
	}
	if o.otelEnabled {
		rt = otelhttp.NewTransport(rt)
	}

	hc := &http.Client{Transport: rt, Timeout: o.timeout}
	rc := resty.NewWithClient(hc).
		SetLogger(slogRestyLogger{logger: o.logger}).
		SetTimeout(o.timeout).
		SetRetryCount(o.retryCount).
		SetRetryWaitTime(o.retryWaitTime).
		SetRetryMaxWaitTime(o.retryMaxWait)
	if o.retryCount > 0 {
		rc.AddRetryConditions(func(r *resty.Response, err error) bool {
			if err != nil {
				return true
			}
			if r == nil {
				return false
			}
			sc := r.StatusCode()
			return sc == http.StatusTooManyRequests || sc >= http.StatusInternalServerError
		})
	}
	if len(o.headers) > 0 {
		rc.SetHeaders(o.headers)
	}
	if o.bearerToken != "" {
		rc.SetAuthToken(o.bearerToken)
	}

	return &Client{baseURL: o.baseURL, resty: rc, transport: transport, logger: o.logger}, nil
}

// Resty returns the underlying *resty.Client for custom streaming or multipart requests.
func (c *Client) Resty() *resty.Client { return c.resty }

// Close closes idle connections held by the underlying HTTP transport and Resty client.
func (c *Client) Close() error {
	err := c.resty.Close()
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
	return err
}

func (c *Client) Get[Resp any](ctx context.Context, path string, opts ...RequestOption) (Resp, error) {
	return doJSON[struct{}, Resp](c, ctx, http.MethodGet, path, nil, false, opts...)
}

func (c *Client) Post[Req, Resp any](ctx context.Context, path string, body Req, opts ...RequestOption) (Resp, error) {
	return doJSON[Req, Resp](c, ctx, http.MethodPost, path, body, true, opts...)
}

func (c *Client) Put[Req, Resp any](ctx context.Context, path string, body Req, opts ...RequestOption) (Resp, error) {
	return doJSON[Req, Resp](c, ctx, http.MethodPut, path, body, true, opts...)
}

func (c *Client) Delete[Resp any](ctx context.Context, path string, opts ...RequestOption) (Resp, error) {
	return doJSON[struct{}, Resp](c, ctx, http.MethodDelete, path, nil, false, opts...)
}

func doJSON[Req, Resp any](
	c *Client,
	ctx context.Context,
	method, path string,
	body any,
	hasBody bool,
	opts ...RequestOption,
) (Resp, error) {
	var zero Resp
	target, err := resolveURL(c.baseURL, path)
	if err != nil {
		return zero, fault.InvalidArgument("invalid request URL").WithCause(err)
	}

	r := c.resty.R().SetContext(ctx).SetHeader("Accept", "application/json")
	if hasBody {
		raw, err := json.Marshal(body)
		if err != nil {
			return zero, fault.InvalidArgument("encode request body failed").WithCause(err)
		}
		r.SetHeader("Content-Type", "application/json").SetBody(raw)
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}

	resp, err := r.Execute(method, target)
	if err != nil {
		return zero, fault.BadGateway("http request failed").WithCause(err)
	}
	if resp.StatusCode() >= http.StatusBadRequest {
		return zero, mapStatusToFault(resp.StatusCode())
	}

	rawResp := resp.Bytes()
	if len(rawResp) == 0 {
		return zero, nil
	}
	var out Resp
	if err := json.Unmarshal(rawResp, &out); err != nil {
		return zero, fault.BadGateway("decode response failed").WithCause(err)
	}
	return out, nil
}

func resolveURL(baseURL, rawPath string) (string, error) {
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") || baseURL == "" {
		return rawPath, nil
	}
	pathPart, query, hasQuery := strings.Cut(rawPath, "?")
	joined, err := url.JoinPath(baseURL, pathPart)
	if err != nil {
		return "", err
	}
	if hasQuery {
		return joined + "?" + query, nil
	}
	return joined, nil
}

func mapStatusToFault(status int) *fault.Error {
	msg := fmt.Sprintf("upstream returned status %d", status)
	switch status {
	case http.StatusBadRequest:
		return fault.BadRequest(msg)
	case http.StatusUnauthorized:
		return fault.Unauthorized(msg)
	case http.StatusForbidden:
		return fault.Forbidden(msg)
	case http.StatusNotFound:
		return fault.NotFound(msg)
	case http.StatusConflict:
		return fault.Conflict(msg)
	case http.StatusTooManyRequests:
		return fault.TooManyRequests(msg)
	case http.StatusBadGateway:
		return fault.BadGateway(msg)
	case http.StatusServiceUnavailable:
		return fault.ServiceUnavailable(msg)
	case http.StatusGatewayTimeout:
		return fault.GatewayTimeout(msg)
	default:
		if status >= 500 {
			return fault.BadGateway(msg).WithStatus(status)
		}
		return fault.BadRequest(msg).WithStatus(status)
	}
}

type slogRestyLogger struct{ logger *slog.Logger }

func (l slogRestyLogger) Errorf(format string, v ...any) {
	l.logger.Error(fmt.Sprintf(format, v...))
}

func (l slogRestyLogger) Warnf(format string, v ...any) {
	l.logger.Warn(fmt.Sprintf(format, v...))
}

func (l slogRestyLogger) Debugf(format string, v ...any) {
	l.logger.Debug(fmt.Sprintf(format, v...))
}
