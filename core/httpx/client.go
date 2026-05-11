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

// Client 是经过包装的 HTTP 客户端，提供：
//   - body 类型驱动的 Content-Type
//   - 基地址、默认请求头、超时
//   - 中间件链 (RoundTripper)
//   - 状态码错误化 (HTTPError)
//   - 可选重试 + 退避
//   - 链路解码 (Into / IntoJSON / IntoXML / IntoBytes / IntoString)
//
// Client 在配置完成后是并发安全的。
type Client struct {
	httpClient    *http.Client
	transport     http.RoundTripper // 用户通过 WithTransport 指定的底层 RoundTripper
	baseURL       string
	defaultHeader http.Header
	middlewares   []Middleware
	errorOnStatus func(int) bool
	retry         retryConfig
}

// retryConfig 重试配置。MaxAttempts <= 1 表示不重试。
type retryConfig struct {
	MaxAttempts int
	Backoff     BackoffFunc
	RetryOn     func(resp *http.Response, err error) bool
}

// BackoffFunc 返回第 attempt 次失败后 (attempt 从 1 开始) 应等待的时长。
type BackoffFunc func(attempt int) time.Duration

// ClientOption 配置 Client。
type ClientOption func(*Client)

// WithHTTPClient 注入自定义 *http.Client。
//
// 注意：如果同时使用了 WithTransport / WithTimeout / 中间件，
// 它们将作用在这个 client 上 (替换其 Transport / Timeout)。
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cli *Client) {
		if c != nil {
			cli.httpClient = c
		}
	}
}

// WithTransport 设置底层 RoundTripper (在中间件链的最内层)。
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(cli *Client) { cli.transport = rt }
}

// WithTimeout 设置请求超时 (整个 Do 调用，包括 body 读取)。
func WithTimeout(d time.Duration) ClientOption {
	return func(cli *Client) { cli.httpClient.Timeout = d }
}

// WithBaseURL 设置基地址，调用时的相对路径会基于此地址解析。
func WithBaseURL(base string) ClientOption {
	return func(cli *Client) { cli.baseURL = strings.TrimRight(base, "/") }
}

// WithDefaultHeader 设置一个默认请求头，发起请求时会被附加 (调用方的 Header 选项会覆盖同名值)。
func WithDefaultHeader(key, value string) ClientOption {
	return func(cli *Client) { cli.defaultHeader.Set(key, value) }
}

// WithMiddleware 注册一个或多个中间件。先注册的位于最外层。
func WithMiddleware(mw ...Middleware) ClientOption {
	return func(cli *Client) { cli.middlewares = append(cli.middlewares, mw...) }
}

// WithErrorOnStatus 自定义"哪些状态码视为错误"。默认 code >= 400。
// 传入 nil 表示禁用状态码错误化，所有响应都不会转成 HTTPError。
func WithErrorOnStatus(fn func(int) bool) ClientOption {
	return func(cli *Client) { cli.errorOnStatus = fn }
}

// WithRetry 启用重试。maxAttempts 包含首次尝试在内 (即 3 表示最多 3 次)。
// backoff 为 nil 时不等待。retryOn 决定何时重试，nil 时使用默认策略
// (网络错误或 5xx / 429)。
//
// 重试要求请求 body 可以重放。本库提供的 Body 构造器 (JSON/XML/Form/Raw/Text/ReadAll)
// 均会先把 body 完整缓存，因此天然支持重试。
func WithRetry(maxAttempts int, backoff BackoffFunc, retryOn func(*http.Response, error) bool) ClientOption {
	return func(cli *Client) {
		cli.retry = retryConfig{
			MaxAttempts: maxAttempts,
			Backoff:     backoff,
			RetryOn:     retryOn,
		}
	}
}

// ExpBackoff 返回指数退避函数：base, 2*base, 4*base, ...
func ExpBackoff(base time.Duration) BackoffFunc {
	return func(attempt int) time.Duration {
		if attempt < 1 {
			attempt = 1
		}
		return base << (attempt - 1)
	}
}

// New 创建一个 Client。
func New(opts ...ClientOption) *Client {
	c := &Client{
		httpClient:    &http.Client{},
		defaultHeader: make(http.Header),
		errorOnStatus: defaultErrorOnStatus,
	}
	for _, opt := range opts {
		opt(c)
	}
	// 装配 transport + 中间件
	c.httpClient.Transport = chain(c.transport, c.middlewares)
	return c
}

// defaultErrorOnStatus 默认错误判定：4xx / 5xx。
func defaultErrorOnStatus(code int) bool { return code >= 400 }

// defaultRetryOn 默认重试判定：网络错误，或 5xx，或 429。
func defaultRetryOn(resp *http.Response, err error) bool {
	if err != nil {
		// context 错误不重试
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

// Do 执行一次 HTTP 请求。
//
// target 可以是绝对 URL，也可以是相对路径 (此时会基于 WithBaseURL 解析)。
// body 为 nil 表示无请求体；否则使用 Body 构造器 (JSON/Form/Raw 等)。
func (c *Client) Do(ctx context.Context, method, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("httpx: nil Context")
	}

	cfg := &requestConfig{
		header: make(http.Header),
		query:  make(url.Values),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// 1. 解析 URL
	fullURL, err := c.resolveURL(target, cfg.query)
	if err != nil {
		return nil, err
	}

	// 2. 编码 body
	var bodyBytes []byte
	var bodyContentType string
	if body != nil {
		bodyBytes, bodyContentType, err = body.Encode()
		if err != nil {
			return nil, err
		}
	}

	// 3. 合并 header (默认 < body 推断 < 用户显式)
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

	// 4. 执行 (含重试)
	resp, err := c.doWithRetry(ctx, method, fullURL, header, bodyBytes)
	if err != nil {
		return nil, err
	}

	// 5. 状态码错误化
	if c.errorOnStatus != nil && c.errorOnStatus(resp.StatusCode) {
		return resp, newHTTPError(method, fullURL, resp)
	}

	// 6. 解码
	if cfg.decode != nil {
		if err := cfg.decode(resp); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

// resolveURL 合并 baseURL + target + query。
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

// doWithRetry 在需要时重试。每次重试都会重建 *http.Request 以便重放 body。
func (c *Client) doWithRetry(ctx context.Context, method, fullURL string, header http.Header, bodyBytes []byte) (*http.Response, error) {
	maxAttempts := c.retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
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
		// 失败的响应必须先排空 + 关闭，才能重用连接
		if lastResp != nil {
			_, _ = io.Copy(io.Discard, lastResp.Body)
			_ = lastResp.Body.Close()
			lastResp = nil
		}
		// 退避
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

// buildRequest 构造单次 *http.Request。
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
		// 提供 GetBody 让 stdlib 在需要时也能重放 (如 307/308 重定向)
		req.ContentLength = int64(len(bodyBytes))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
	return req, nil
}

// newHTTPError 从响应构造 HTTPError，并消费 body (限长保存)。
func newHTTPError(method, fullURL string, resp *http.Response) *HTTPError {
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, ErrorBodyLimit)
	data, _ := io.ReadAll(limited)
	// 排空剩余字节以便复用连接
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

// Get 发送 GET 请求。
func (c *Client) Get(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodGet, target, nil, opts...)
}

// Head 发送 HEAD 请求。
func (c *Client) Head(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodHead, target, nil, opts...)
}

// Post 发送 POST 请求。
func (c *Client) Post(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodPost, target, body, opts...)
}

// Put 发送 PUT 请求。
func (c *Client) Put(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodPut, target, body, opts...)
}

// Patch 发送 PATCH 请求。
func (c *Client) Patch(ctx context.Context, target string, body Body, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodPatch, target, body, opts...)
}

// Delete 发送 DELETE 请求。
func (c *Client) Delete(ctx context.Context, target string, opts ...RequestOption) (*http.Response, error) {
	return c.Do(ctx, http.MethodDelete, target, nil, opts...)
}
