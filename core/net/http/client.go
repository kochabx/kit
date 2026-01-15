package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"sync"
)

const (
	// Buffer pool constants
	defaultBufferSize = 4096
	maxBufferSize     = 1024 * 1024 // 1MB
)

// Client represents an HTTP client with connection pooling and request optimization
type Client struct {
	client         *http.Client
	requestOptPool sync.Pool
	bufferPool     sync.Pool
}

// Option configures the HTTP client
type Option func(*Client)

// WithClient sets a custom HTTP client
func WithClient(client *http.Client) Option {
	return func(h *Client) {
		h.client = client
	}
}

// New creates a new optimized HTTP client with object pooling
func New(opts ...Option) *Client {
	h := &Client{
		client: &http.Client{},
		requestOptPool: sync.Pool{
			New: func() any {
				return &RequestOption{
					header: make(map[string]string, 8), // Pre-allocate with reasonable capacity
				}
			},
		},
		bufferPool: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
			},
		},
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

// RequestOption holds options for individual HTTP requests
type RequestOption struct {
	ctx      context.Context
	header   map[string]string
	response any
}

// WithContext sets a custom context for the request
func WithContext(ctx context.Context) func(*RequestOption) {
	return func(opt *RequestOption) {
		opt.ctx = ctx
	}
}

// WithHeader sets multiple headers for the request
func WithHeader(header map[string]string) func(*RequestOption) {
	return func(opt *RequestOption) {
		maps.Copy(opt.header, header)
	}
}

// WithResponse sets the response target object for automatic unmarshaling
func WithResponse(response any) func(*RequestOption) {
	return func(opt *RequestOption) {
		opt.response = response
	}
}

// reset efficiently resets the RequestOption for reuse
func (opt *RequestOption) reset() {
	opt.ctx = nil
	// Clear map efficiently by reusing the underlying storage
	for k := range opt.header {
		delete(opt.header, k)
	}
	// Set default content type
	opt.header["Content-Type"] = ContentTypeJSON
	opt.response = nil
}

// Request sends an HTTP request with the specified method, URL, and body
func (cli *Client) Request(method, url string, body any, opts ...func(*RequestOption)) (*http.Response, error) {
	// Get and configure request options
	opt := cli.getRequestOption()
	defer cli.putRequestOption(opt)

	// Apply request options
	for _, o := range opts {
		o(opt)
	}

	// Create HTTP request
	req, err := cli.createRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// Set headers and context
	cli.setRequestHeaders(req, opt.header)
	if opt.ctx != nil {
		req = req.WithContext(opt.ctx)
	}

	// Execute request
	resp, err := cli.client.Do(req)
	if err != nil {
		return nil, err
	}

	// Process response
	return cli.processResponse(resp, opt.response)
}

// getRequestOption retrieves a RequestOption from the pool
func (cli *Client) getRequestOption() *RequestOption {
	opt := cli.requestOptPool.Get().(*RequestOption)
	opt.reset()
	return opt
}

// putRequestOption returns a RequestOption to the pool
func (cli *Client) putRequestOption(opt *RequestOption) {
	cli.requestOptPool.Put(opt)
}

// createRequest creates an HTTP request with the appropriate body
func (cli *Client) createRequest(method, url string, body any) (*http.Request, error) {
	switch v := body.(type) {
	case nil:
		return http.NewRequest(method, url, nil)
	case io.Reader:
		return http.NewRequest(method, url, v)
	default:
		return cli.createJSONRequest(method, url, v)
	}
}

// createJSONRequest creates an HTTP request with JSON body
func (cli *Client) createJSONRequest(method, url string, body any) (*http.Request, error) {
	buf := cli.getBuffer()
	defer cli.putBuffer(buf)

	if err := json.NewEncoder(buf).Encode(body); err != nil {
		return nil, err
	}

	return http.NewRequest(method, url, bytes.NewReader(buf.Bytes()))
}

// setRequestHeaders sets headers on the HTTP request
func (cli *Client) setRequestHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// getBuffer retrieves a buffer from the pool
func (cli *Client) getBuffer() *bytes.Buffer {
	buf := cli.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// putBuffer returns a buffer to the pool, with size check to prevent memory leaks
func (cli *Client) putBuffer(buf *bytes.Buffer) {
	// Prevent very large buffers from being pooled to avoid memory leaks
	if buf.Cap() <= maxBufferSize {
		cli.bufferPool.Put(buf)
	}
}

// processResponse processes the HTTP response and handles errors
func (cli *Client) processResponse(resp *http.Response, dest any) (*http.Response, error) {
	if dest == nil {
		return resp, nil
	}

	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return nil, err
	}

	return resp, nil
}

// Convenience methods for common HTTP operations

// Get performs a GET request
func (cli *Client) Get(url string, opts ...func(*RequestOption)) (*http.Response, error) {
	return cli.Request(MethodGet, url, nil, opts...)
}

// Post performs a POST request with JSON body
func (cli *Client) Post(url string, body any, opts ...func(*RequestOption)) (*http.Response, error) {
	return cli.Request(MethodPost, url, body, opts...)
}

// Put performs a PUT request with JSON body
func (cli *Client) Put(url string, body any, opts ...func(*RequestOption)) (*http.Response, error) {
	return cli.Request(MethodPut, url, body, opts...)
}

// Delete performs a DELETE request
func (cli *Client) Delete(url string, opts ...func(*RequestOption)) (*http.Response, error) {
	return cli.Request(MethodDelete, url, nil, opts...)
}

// Patch performs a PATCH request with JSON body
func (cli *Client) Patch(url string, body any, opts ...func(*RequestOption)) (*http.Response, error) {
	return cli.Request(MethodPatch, url, body, opts...)
}
