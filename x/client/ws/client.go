// Package ws provides an ordered, auto-reconnecting WebSocket client backed
// by coder/websocket, cenkalti/backoff/v5, iter.Seq2, and sync.WaitGroup.Go.
package ws

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
)

type MessageType = websocket.MessageType

const (
	MessageText   MessageType = websocket.MessageText
	MessageBinary MessageType = websocket.MessageBinary
)

// Message represents a single text or binary WebSocket frame.
type Message struct {
	Type MessageType
	Data []byte
}

var (
	ErrInvalidURL = errors.New("invalid websocket URL")
	ErrClosed     = errors.New("websocket client closed")
)

type options struct {
	dialTimeout    time.Duration
	pingInterval   time.Duration
	headers        http.Header
	tlsConfig      *tls.Config
	reconnect      bool
	initialBackoff time.Duration
	maxBackoff     time.Duration
	maxRetries     int // 0 means unlimited while ctx active when reconnect=true
	logger         *slog.Logger
}

// Option customizes Client construction.
type Option func(*options)

func WithDialTimeout(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.dialTimeout = d
		}
	}
}

func WithPingInterval(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.pingInterval = d
		}
	}
}

func WithHeaders(h http.Header) Option {
	return func(o *options) {
		if h != nil {
			o.headers = h.Clone()
		}
	}
}

func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(o *options) {
		if tlsCfg != nil {
			o.tlsConfig = tlsCfg.Clone()
		}
	}
}

func WithReconnect(enabled bool, initial, maxInterval time.Duration, maxRetries int) Option {
	return func(o *options) {
		o.reconnect = enabled
		if initial > 0 {
			o.initialBackoff = initial
		}
		if maxInterval > 0 {
			o.maxBackoff = maxInterval
		}
		o.maxRetries = maxRetries
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// Client manages ordered WebSocket reads, backoff reconnects, heartbeat pings,
// and clean RFC 6455 shutdown.
type Client struct {
	targetURL string
	opts      options
	dialOpts  *websocket.DialOptions

	mu         sync.Mutex
	conn       *websocket.Conn
	connCancel context.CancelFunc
	closed     atomic.Bool
	wg         sync.WaitGroup
	closeOnce  func() error
}

// New validates targetURL (ws:// or wss://) and builds a Client without dialing yet.
func New(targetURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(targetURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidURL, targetURL)
	}
	o := options{
		dialTimeout:    10 * time.Second,
		pingInterval:   0,
		reconnect:      true,
		initialBackoff: 100 * time.Millisecond,
		maxBackoff:     10 * time.Second,
		logger:         slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	dialOpts := &websocket.DialOptions{HTTPHeader: o.headers}
	if o.tlsConfig != nil {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok || base == nil {
			return nil, fmt.Errorf("ws: cannot apply TLSConfig to non-*http.Transport DefaultTransport (%T)", http.DefaultTransport)
		}
		tr := base.Clone()
		tr.TLSClientConfig = o.tlsConfig
		dialOpts.HTTPClient = &http.Client{Transport: tr}
	}

	c := &Client{targetURL: targetURL, opts: o, dialOpts: dialOpts}
	c.closeOnce = sync.OnceValue(c.closeInternal)
	return c, nil
}

// Connect dials the WebSocket server and starts the optional heartbeat loop.
func (c *Client) Connect(ctx context.Context) error {
	if c.closed.Load() {
		return ErrClosed
	}
	_, err := c.dial(ctx)
	return err
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.opts.dialTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, c.targetURL, c.dialOpts)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "client closed")
		return nil, ErrClosed
	}
	oldConn, oldCancel := c.conn, c.connCancel
	connCtx, connCancel := context.WithCancel(context.Background())
	c.conn = conn
	c.connCancel = connCancel
	if c.opts.pingInterval > 0 {
		pingInterval := c.opts.pingInterval
		c.wg.Go(func() {
			ticker := time.NewTicker(pingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-connCtx.Done():
					return
				case <-ticker.C:
					pingCtx, pingCancel := context.WithTimeout(connCtx, pingInterval)
					err := conn.Ping(pingCtx)
					pingCancel()
					if err != nil && connCtx.Err() == nil {
						c.opts.logger.Warn("websocket ping failed", slog.Any("error", err))
						_ = conn.CloseNow()
						return
					}
				}
			}
		})
	}
	c.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldConn != nil {
		_ = oldConn.CloseNow()
	}
	return conn, nil
}

// Send writes msg to the currently active WebSocket connection.
func (c *Client) Send(ctx context.Context, msg Message) error {
	if c.closed.Load() {
		return ErrClosed
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("websocket not connected")
	}
	return conn.Write(ctx, msg.Type, msg.Data)
}

// Messages yields incoming messages strictly in FIFO read order using Go iter.Seq2,
// automatically reconnecting with exponential backoff via cenkalti/backoff/v5 when enabled.
func (c *Client) Messages(ctx context.Context) iter.Seq2[Message, error] {
	return func(yield func(Message, error) bool) {
		bo := backoff.NewExponentialBackOff()
		bo.InitialInterval = c.opts.initialBackoff
		bo.MaxInterval = c.opts.maxBackoff
		bo.Reset()
		retries := 0

		for {
			if ctx.Err() != nil || c.closed.Load() {
				return
			}

			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				var err error
				conn, err = c.dial(ctx)
				if err != nil {
					if !c.opts.reconnect || !c.shouldRetry(&retries, ctx, err, bo, yield) {
						if !c.opts.reconnect && ctx.Err() == nil && !c.closed.Load() {
							_ = yield(Message{}, err)
						}
						return
					}
					continue
				}
			}

			typ, data, err := conn.Read(ctx)
			if err == nil {
				bo.Reset()
				retries = 0
				if !yield(Message{Type: typ, Data: data}, nil) {
					return
				}
				continue
			}

			c.clearConn(conn)
			if ctx.Err() != nil || c.closed.Load() || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}

			if !c.opts.reconnect || !c.shouldRetry(&retries, ctx, err, bo, yield) {
				if !c.opts.reconnect {
					_ = yield(Message{}, err)
				}
				return
			}
		}
	}
}

func (c *Client) shouldRetry(
	retries *int,
	ctx context.Context,
	readErr error,
	bo *backoff.ExponentialBackOff,
	yield func(Message, error) bool,
) bool {
	if c.opts.maxRetries > 0 && *retries >= c.opts.maxRetries {
		_ = yield(Message{}, readErr)
		return false
	}
	*retries++
	delay := bo.NextBackOff()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return !c.closed.Load()
	}
}

func (c *Client) clearConn(target *websocket.Conn) {
	c.mu.Lock()
	var cancel context.CancelFunc
	if c.conn == target {
		c.conn = nil
		cancel = c.connCancel
		c.connCancel = nil
	}
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = target.CloseNow()
}

// Close performs a graceful RFC 6455 close handshake (StatusNormalClosure) and waits for workers.
func (c *Client) Close() error { return c.closeOnce() }

func (c *Client) closeInternal() error {
	c.mu.Lock()
	c.closed.Store(true)
	conn := c.conn
	cancel := c.connCancel
	c.conn = nil
	c.connCancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var closeErr error
	if conn != nil {
		closeErr = conn.Close(websocket.StatusNormalClosure, "client closed")
	}
	c.wg.Wait()
	return closeErr
}
