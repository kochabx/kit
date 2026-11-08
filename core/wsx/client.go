package wsx

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// connectionSession owns one Connect lifecycle, including automatic reconnects.
type connectionSession struct {
	ctx         context.Context
	cancel      context.CancelFunc
	stopClient  func() bool
	stopContext func() bool
	url         string
	writeChan   chan Message
}

// Client implements the WebSocket client exposed through Clienter.
// The zero value is not usable; construct clients with New.
type Client struct {
	config    Config
	headers   http.Header
	dialer    *websocket.Dialer
	tlsConfig *tls.Config

	// Lifecycle state.
	ctx    context.Context
	cancel context.CancelFunc
	close  func()

	// Runtime state protected by mu.
	mu           sync.RWMutex
	session      *connectionSession
	conn         *websocket.Conn
	connCancel   context.CancelFunc
	handlers     map[EventType][]EventHandler
	connected    bool
	closed       bool
	reconnecting bool
	retryCount   int
}

// Verify at compile time that *Client implements Clienter.
var _ Clienter = (*Client)(nil)

// New creates a WebSocket client.
func New(opts ...Option) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		config:   DefaultConfig(),
		handlers: make(map[EventType][]EventHandler),
		ctx:      ctx,
		cancel:   cancel,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.dialer == nil {
		c.dialer = &websocket.Dialer{
			HandshakeTimeout:  c.config.HandshakeTimeout,
			ReadBufferSize:    c.config.ReadBufferSize,
			WriteBufferSize:   c.config.WriteBufferSize,
			EnableCompression: c.config.EnableCompression,
		}
	}
	if c.tlsConfig != nil {
		c.dialer.TLSClientConfig = c.tlsConfig
	}

	c.close = sync.OnceFunc(c.closeResources)
	return c
}

// Connect implements Clienter.Connect.
func (c *Client) Connect(ctx context.Context, wsURL string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}
	if c.session != nil {
		c.mu.Unlock()
		return ErrAlreadyConnected
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrInvalidScheme, u.Scheme)
	}
	if ctx == nil {
		ctx = c.ctx
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &connectionSession{
		ctx:       sessionCtx,
		cancel:    cancel,
		url:       wsURL,
		writeChan: make(chan Message, c.config.WriteQueueSize),
	}
	c.session = session
	c.retryCount = 0
	c.reconnecting = true
	session.stopClient = context.AfterFunc(c.ctx, cancel)
	session.stopContext = context.AfterFunc(sessionCtx, func() { c.endSession(session) })
	c.mu.Unlock()
	if err := c.dial(session); err != nil {
		c.endSession(session)
		return err
	}
	return nil
}

// dial performs one handshake within the same caller-owned connection session.
func (c *Client) dial(session *connectionSession) error {
	conn, _, err := c.dialContext(session.ctx, session.url)
	if err != nil {
		if session.ctx.Err() != nil {
			return session.ctx.Err()
		}
		c.emitEvent(Event{Type: EventError, Error: fmt.Errorf("wsx: dial failed: %w", err), Timestamp: time.Now()})
		return err
	}
	connCtx, connCancel := context.WithCancel(session.ctx)
	conn.SetReadLimit(c.config.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(c.config.PongWait)) })
	c.mu.Lock()
	if c.closed || c.session != session || session.ctx.Err() != nil {
		closed := c.closed
		c.mu.Unlock()
		connCancel()
		_ = conn.Close()
		if closed {
			return ErrClientClosed
		}
		if err := session.ctx.Err(); err != nil {
			return err
		}
		return context.Canceled
	}
	c.conn = conn
	c.connected = true
	c.reconnecting = false
	c.connCancel = connCancel
	c.mu.Unlock()
	go c.readLoop(conn, connCtx, connCancel, session)
	go c.writeLoop(conn, connCtx, session.writeChan)
	if c.config.PingInterval > 0 {
		go c.pingLoop(conn, connCtx)
	}
	c.emitEvent(Event{Type: EventConnected, Timestamp: time.Now()})
	return nil
}

// readLoop cancels the connection loops and calls onConnectionLost on exit.
func (c *Client) readLoop(conn *websocket.Conn, ctx context.Context, cancel context.CancelFunc, session *connectionSession) {
	defer cancel()
	defer c.onConnectionLost(session, conn)

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil {
				c.emitEvent(Event{
					Type:      EventError,
					Error:     fmt.Errorf("wsx: read: %w", err),
					Timestamp: time.Now(),
				})
			}
			return
		}

		if messageType == websocket.CloseMessage {
			return
		}

		c.emitEvent(Event{
			Type: EventMessage,
			Data: Message{
				Type: MessageType(messageType),
				Data: data,
			},
			Timestamp: time.Now(),
		})
	}
}

// writeLoop consumes the session queue using its own connection.
func (c *Client) writeLoop(conn *websocket.Conn, ctx context.Context, queue <-chan Message) {
	defer conn.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-queue:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			if err := conn.WriteMessage(int(msg.Type), msg.Data); err != nil {
				if ctx.Err() == nil {
					c.emitEvent(Event{
						Type:      EventError,
						Error:     fmt.Errorf("wsx: write: %w", err),
						Timestamp: time.Now(),
					})
				}
				return
			}
		}
	}
}

// pingLoop sends control frames directly, without using the message queue.
func (c *Client) pingLoop(conn *websocket.Conn, ctx context.Context) {
	defer conn.Close()
	t := time.NewTicker(c.config.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			deadline := time.Now().Add(c.config.WriteTimeout)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				if ctx.Err() == nil {
					c.emitEvent(Event{
						Type:      EventError,
						Error:     fmt.Errorf("wsx: ping: %w", err),
						Timestamp: time.Now(),
					})
				}
				return
			}
		}
	}
}

// onConnectionLost only clears the connection owned by this session and read loop.
func (c *Client) onConnectionLost(session *connectionSession, conn *websocket.Conn) {
	c.mu.Lock()
	if c.session != session || c.conn != conn {
		c.mu.Unlock()
		return
	}
	c.connected = false
	c.conn = nil
	if c.connCancel != nil {
		c.connCancel()
		c.connCancel = nil
	}
	reconnect := !c.closed && session.ctx.Err() == nil && c.config.Reconnect.Enable
	c.reconnecting = reconnect
	c.mu.Unlock()
	_ = conn.Close()
	c.emitEvent(Event{Type: EventDisconnected, Timestamp: time.Now()})
	if reconnect {
		go c.runReconnect(session)
	} else {
		c.endSession(session)
	}
}

// runReconnect shares the original Connect context, including its cancellation and values.
func (c *Client) runReconnect(session *connectionSession) {
	for {
		c.mu.Lock()
		if c.closed || c.session != session || session.ctx.Err() != nil {
			c.mu.Unlock()
			return
		}
		rc := c.config.Reconnect
		if rc.MaxRetries > 0 && c.retryCount >= rc.MaxRetries {
			c.mu.Unlock()
			c.emitEvent(Event{Type: EventError, Error: ErrMaxRetriesExceeded, Timestamp: time.Now()})
			c.endSession(session)
			return
		}
		c.retryCount++
		attempt := c.retryCount
		c.mu.Unlock()
		delay := c.computeBackoff(attempt)
		c.emitEvent(Event{
			Type: EventReconnecting,
			Data: map[string]any{
				"attempt": attempt,
				"delay":   delay,
			},
			Timestamp: time.Now(),
		})
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-session.ctx.Done():
			timer.Stop()
			return
		}
		if err := c.dial(session); err == nil {
			c.mu.Lock()
			if c.session == session {
				c.retryCount = 0
			}
			c.mu.Unlock()
			return
		}
	}
}

// endSession stops pending handshakes, retries and socket I/O without closing the client.
func (c *Client) endSession(session *connectionSession) {
	if session == nil {
		return
	}
	c.mu.Lock()
	if c.session != session {
		c.mu.Unlock()
		return
	}
	conn, cancel, connected := c.conn, c.connCancel, c.connected
	c.session, c.conn, c.connCancel = nil, nil, nil
	c.connected, c.reconnecting = false, false
	// Cancel before admitting a new Connect so old session work cannot be published.
	session.cancel()
	session.stopClient()
	session.stopContext()
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if connected {
		c.emitEvent(Event{Type: EventDisconnected, Timestamp: time.Now()})
	}
}

func (c *Client) computeBackoff(attempt int) time.Duration {
	rc := c.config.Reconnect
	if attempt <= 1 {
		return rc.Interval
	}
	d := float64(rc.Interval)
	for i := 1; i < attempt; i++ {
		d *= rc.BackoffMultiplier
		if rc.MaxInterval > 0 && time.Duration(d) >= rc.MaxInterval {
			return rc.MaxInterval
		}
	}
	return time.Duration(d)
}

// Send implements Clienter.Send.
func (c *Client) Send(messageType MessageType, data []byte) error {
	c.mu.RLock()
	closed := c.closed
	connected := c.connected
	session := c.session
	c.mu.RUnlock()

	if closed {
		return ErrClientClosed
	}
	if !connected {
		return ErrNotConnected
	}

	timer := time.NewTimer(c.config.WriteTimeout)
	defer timer.Stop()

	select {
	case session.writeChan <- Message{Type: messageType, Data: data}:
		return nil
	case <-timer.C:
		return ErrSendTimeout
	case <-c.ctx.Done():
		return ErrClientClosed
	case <-session.ctx.Done():
		if c.ctx.Err() != nil {
			return ErrClientClosed
		}
		return ErrNotConnected
	}
}

// SendText implements Clienter.SendText.
func (c *Client) SendText(text string) error {
	return c.Send(TextMessage, []byte(text))
}

// SendBinary implements Clienter.SendBinary.
func (c *Client) SendBinary(data []byte) error {
	return c.Send(BinaryMessage, data)
}

// Disconnect implements Clienter.Disconnect.
func (c *Client) Disconnect() error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()
	c.endSession(session)
	return nil
}

// Close implements Clienter.Close.
func (c *Client) Close() error {
	c.close()
	return nil
}

func (c *Client) closeResources() {
	c.mu.Lock()
	c.closed = true
	session := c.session
	c.mu.Unlock()
	c.cancel()
	c.endSession(session)
}

// OnEvent implements Clienter.OnEvent.
func (c *Client) OnEvent(eventType EventType, handler EventHandler) {
	if handler == nil {
		return
	}
	c.mu.Lock()
	c.handlers[eventType] = append(c.handlers[eventType], handler)
	c.mu.Unlock()
}

// RemoveEventHandler implements Clienter.RemoveEventHandler.
func (c *Client) RemoveEventHandler(eventType EventType) {
	c.mu.Lock()
	delete(c.handlers, eventType)
	c.mu.Unlock()
}

// IsConnected implements Clienter.IsConnected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Config implements Clienter.Config.
func (c *Client) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// emitEvent dispatches callbacks asynchronously to avoid blocking read and write loops.
// Copy handlers under RLock so concurrent registration cannot change the active slice.
func (c *Client) emitEvent(event Event) {
	c.mu.RLock()
	handlers := c.handlers[event.Type]
	if len(handlers) == 0 {
		c.mu.RUnlock()
		return
	}
	cp := make([]EventHandler, len(handlers))
	copy(cp, handlers)
	c.mu.RUnlock()

	go func() {
		for _, h := range cp {
			h(event)
		}
	}()
}
