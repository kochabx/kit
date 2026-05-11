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

// Client WebSocket 客户端实现，对外通过 Clienter 接口暴露。
// 零值不可用，必须经由 New 构造。
type Client struct {
	config    Config
	headers   http.Header
	dialer    *websocket.Dialer
	tlsConfig *tls.Config

	// 生命周期
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once

	// 受 mu 保护的运行时状态
	mu                    sync.RWMutex
	url                   string
	conn                  *websocket.Conn
	connCancel            context.CancelFunc
	handlers              map[EventType][]EventHandler
	connected             bool
	closed                bool
	reconnecting          bool
	intentionalDisconnect bool
	retryCount            int

	// 写队列；New 中按 WriteQueueSize 创建
	writeChan chan Message
}

// 编译期保证 *Client 满足 Clienter
var _ Clienter = (*Client)(nil)

// New 构造一个 WebSocket 客户端。
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

	if c.writeChan == nil {
		c.writeChan = make(chan Message, c.config.WriteQueueSize)
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

	return c
}

// Connect 见 Clienter.Connect。
func (c *Client) Connect(ctx context.Context, wsURL string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}
	if c.connected || c.reconnecting {
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

	c.url = wsURL
	c.retryCount = 0
	c.intentionalDisconnect = false
	c.mu.Unlock()

	return c.dial(ctx)
}

// dial 执行一次握手，成功后启动读/写/ping 循环。
func (c *Client) dial(ctx context.Context) error {
	if ctx == nil {
		ctx = c.ctx
	}

	conn, _, err := c.dialer.DialContext(ctx, c.url, c.headers)
	if err != nil {
		c.emitEvent(Event{
			Type:      EventError,
			Error:     fmt.Errorf("wsx: dial failed: %w", err),
			Timestamp: time.Now(),
		})
		return err
	}

	connCtx, connCancel := context.WithCancel(c.ctx)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		connCancel()
		_ = conn.Close()
		return ErrClientClosed
	}
	c.conn = conn
	c.connected = true
	c.reconnecting = false
	c.connCancel = connCancel
	c.mu.Unlock()

	conn.SetReadLimit(c.config.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	})

	go c.readLoop(conn, connCtx, connCancel)
	go c.writeLoop(conn, connCtx)
	if c.config.PingInterval > 0 {
		go c.pingLoop(conn, connCtx)
	}

	c.emitEvent(Event{Type: EventConnected, Timestamp: time.Now()})
	return nil
}

// readLoop 读循环，退出时通过 connCancel 通知其它循环并触发 onConnectionLost。
func (c *Client) readLoop(conn *websocket.Conn, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	defer c.onConnectionLost()

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

// writeLoop 仅在持有自身 conn 的前提下消费 writeChan。
func (c *Client) writeLoop(conn *websocket.Conn, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.writeChan:
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

// pingLoop 通过 WriteControl 直接发送 ping，绕开 writeChan 以避免相互阻塞。
func (c *Client) pingLoop(conn *websocket.Conn, ctx context.Context) {
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

// onConnectionLost 在 readLoop 退出后被调用，负责状态清理及触发重连。
func (c *Client) onConnectionLost() {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return
	}
	c.connected = false
	conn := c.conn
	c.conn = nil
	if c.connCancel != nil {
		c.connCancel()
		c.connCancel = nil
	}
	intentional := c.intentionalDisconnect
	c.intentionalDisconnect = false
	closed := c.closed
	enableReconnect := c.config.Reconnect.Enable
	c.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}

	c.emitEvent(Event{Type: EventDisconnected, Timestamp: time.Now()})

	if !intentional && !closed && enableReconnect {
		go c.runReconnect()
	}
}

// runReconnect 在独立 goroutine 中实现带退避的自动重连。
func (c *Client) runReconnect() {
	for {
		c.mu.Lock()
		if c.closed || c.intentionalDisconnect {
			c.mu.Unlock()
			return
		}
		rc := c.config.Reconnect
		if rc.MaxRetries > 0 && c.retryCount >= rc.MaxRetries {
			c.mu.Unlock()
			c.emitEvent(Event{
				Type:      EventError,
				Error:     ErrMaxRetriesExceeded,
				Timestamp: time.Now(),
			})
			return
		}
		c.reconnecting = true
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
		case <-c.ctx.Done():
			timer.Stop()
			return
		}

		if err := c.dial(c.ctx); err == nil {
			c.mu.Lock()
			c.retryCount = 0
			c.mu.Unlock()
			return
		}
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

// Send 见 Clienter.Send。
func (c *Client) Send(messageType MessageType, data []byte) error {
	c.mu.RLock()
	closed := c.closed
	connected := c.connected
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
	case c.writeChan <- Message{Type: messageType, Data: data}:
		return nil
	case <-timer.C:
		return ErrSendTimeout
	case <-c.ctx.Done():
		return ErrClientClosed
	}
}

// SendText 见 Clienter.SendText。
func (c *Client) SendText(text string) error {
	return c.Send(TextMessage, []byte(text))
}

// SendBinary 见 Clienter.SendBinary。
func (c *Client) SendBinary(data []byte) error {
	return c.Send(BinaryMessage, data)
}

// Disconnect 见 Clienter.Disconnect。
func (c *Client) Disconnect() error {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil
	}
	c.intentionalDisconnect = true
	conn := c.conn
	cancel := c.connCancel
	c.mu.Unlock()

	if conn != nil {
		// 发送关闭帧，忽略错误，对端可能已经先关
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		_ = conn.Close()
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

// Close 见 Clienter.Close。
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.intentionalDisconnect = true
		conn := c.conn
		cancel := c.connCancel
		c.mu.Unlock()

		c.cancel()

		if conn != nil {
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			)
			_ = conn.Close()
		}
		if cancel != nil {
			cancel()
		}
	})
	return nil
}

// OnEvent 见 Clienter.OnEvent。
func (c *Client) OnEvent(eventType EventType, handler EventHandler) {
	if handler == nil {
		return
	}
	c.mu.Lock()
	c.handlers[eventType] = append(c.handlers[eventType], handler)
	c.mu.Unlock()
}

// RemoveEventHandler 见 Clienter.RemoveEventHandler。
func (c *Client) RemoveEventHandler(eventType EventType) {
	c.mu.Lock()
	delete(c.handlers, eventType)
	c.mu.Unlock()
}

// IsConnected 见 Clienter.IsConnected。
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Config 见 Clienter.Config。
func (c *Client) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// emitEvent 异步派发事件，避免阻塞 read/write 循环。
// 在 RLock 内复制处理器切片，避免在调用期间被并发修改。
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
