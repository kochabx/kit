package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsClient WebSocket客户端实现
type wsClient struct {
	config   Config
	conn     *websocket.Conn
	url      string
	headers  http.Header
	mu       sync.RWMutex
	handlers map[EventType][]EventHandler

	// 状态管理
	connected    bool
	reconnecting bool
	shouldClose  bool

	// 重连相关
	retryCount int
	retryTimer *time.Timer

	// goroutine控制
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	writeChan chan Message

	// ping/pong
	pingTicker *time.Ticker

	dialer *websocket.Dialer
}

// NewClient 创建新的WebSocket客户端
func NewClient(opts ...wsClientOption) Client {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := defaultConfig()
	client := &wsClient{
		config:    cfg,
		handlers:  make(map[EventType][]EventHandler),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		writeChan: make(chan Message, 100),
		dialer: &websocket.Dialer{
			HandshakeTimeout:  cfg.ConnectTimeout,
			ReadBufferSize:    cfg.ReadBufferSize,
			WriteBufferSize:   cfg.WriteBufferSize,
			EnableCompression: cfg.EnableCompression,
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Connect 连接到WebSocket服务器
func (c *wsClient) Connect(ctx context.Context, wsURL string) error {
	c.mu.Lock()

	if c.connected {
		c.mu.Unlock()
		return fmt.Errorf("client already connected")
	}

	// 验证URL
	u, err := url.Parse(wsURL)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("invalid websocket URL: %w", err)
	}

	if u.Scheme != "ws" && u.Scheme != "wss" {
		c.mu.Unlock()
		return fmt.Errorf("invalid websocket scheme: %s", u.Scheme)
	}

	c.url = wsURL
	c.shouldClose = false
	c.retryCount = 0
	c.mu.Unlock()

	return c.connect()
}

// connect 内部连接方法
func (c *wsClient) connect() error {
	// 建立连接
	conn, _, err := c.dialer.Dial(c.url, c.headers)
	if err != nil {
		c.emitEvent(Event{
			Type:      EventError,
			Error:     fmt.Errorf("failed to connect: %w", err),
			Timestamp: time.Now(),
		})
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.reconnecting = false
	c.mu.Unlock()

	// 设置连接选项
	c.conn.SetReadLimit(c.config.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
		return nil
	})

	// 启动ping定时器
	c.startPingTimer()

	// 启动读写goroutines
	go c.readLoop()
	go c.writeLoop()

	// 发射连接事件
	c.emitEvent(Event{
		Type:      EventConnected,
		Timestamp: time.Now(),
	})

	return nil
}

// Disconnect 断开连接
func (c *wsClient) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.shouldClose = true

	if c.retryTimer != nil {
		c.retryTimer.Stop()
		c.retryTimer = nil
	}

	return c.closeConnection()
}

// closeConnection 关闭连接
func (c *wsClient) closeConnection() error {
	if !c.connected {
		return nil
	}

	c.connected = false

	// 停止ping定时器
	c.stopPingTimer()

	// 发送关闭消息
	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
		c.conn = nil
	}

	// 发射断开连接事件
	c.emitEvent(Event{
		Type:      EventDisconnected,
		Timestamp: time.Now(),
	})

	return nil
}

// Send 发送消息
func (c *wsClient) Send(messageType MessageType, data []byte) error {
	if !c.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	select {
	case c.writeChan <- Message{Type: messageType, Data: data}:
		return nil
	case <-time.After(c.config.WriteTimeout):
		return fmt.Errorf("send timeout")
	}
}

// SendText 发送文本消息
func (c *wsClient) SendText(text string) error {
	return c.Send(TextMessage, []byte(text))
}

// SendBinary 发送二进制消息
func (c *wsClient) SendBinary(data []byte) error {
	return c.Send(BinaryMessage, data)
}

// OnEvent 注册事件处理器
func (c *wsClient) OnEvent(eventType EventType, handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers[eventType] = append(c.handlers[eventType], handler)
}

// RemoveEventHandler 移除事件处理器
func (c *wsClient) RemoveEventHandler(eventType EventType) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.handlers, eventType)
}

// IsConnected 检查是否已连接
func (c *wsClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetConfig 获取配置
func (c *wsClient) GetConfig() Config {
	return c.config
}

// Close 关闭客户端
func (c *wsClient) Close() error {
	c.cancel()

	if err := c.Disconnect(); err != nil {
		return err
	}

	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		// 超时强制关闭
	}

	return nil
}

// readLoop 读取循环
func (c *wsClient) readLoop() {
	defer func() {
		c.mu.Lock()
		connected := c.connected
		shouldReconnect := c.config.ReconnectConfig.Enable && !c.shouldClose
		c.mu.Unlock()

		if connected {
			c.closeConnection()
		}

		if shouldReconnect {
			c.scheduleReconnect()
		} else {
			close(c.done)
		}
	}()

	for {
		if !c.IsConnected() {
			return
		}

		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			c.emitEvent(Event{
				Type:      EventError,
				Error:     fmt.Errorf("read message error: %w", err),
				Timestamp: time.Now(),
			})
			return
		}

		// 处理特殊消息类型
		switch messageType {
		case websocket.CloseMessage:
			return
		case websocket.PingMessage:
			c.conn.WriteMessage(websocket.PongMessage, nil)
			continue
		}

		// 发射消息事件
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

// writeLoop 写入循环
func (c *wsClient) writeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-c.writeChan:
			if !c.IsConnected() {
				continue
			}

			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			err := c.conn.WriteMessage(int(msg.Type), msg.Data)
			if err != nil {
				c.emitEvent(Event{
					Type:      EventError,
					Error:     fmt.Errorf("write message error: %w", err),
					Timestamp: time.Now(),
				})
			}
		}
	}
}

// startPingTimer 启动ping定时器
func (c *wsClient) startPingTimer() {
	if c.pingTicker != nil {
		c.pingTicker.Stop()
	}

	c.pingTicker = time.NewTicker(c.config.PingInterval)

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-c.pingTicker.C:
				if c.IsConnected() {
					c.Send(PingMessage, nil)
				}
			}
		}
	}()
}

// stopPingTimer 停止ping定时器
func (c *wsClient) stopPingTimer() {
	if c.pingTicker != nil {
		c.pingTicker.Stop()
		c.pingTicker = nil
	}
}

// scheduleReconnect 安排重连
func (c *wsClient) scheduleReconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.shouldClose || !c.config.ReconnectConfig.Enable {
		close(c.done)
		return
	}

	// 检查重连次数限制
	if c.config.ReconnectConfig.MaxRetries > 0 && c.retryCount >= c.config.ReconnectConfig.MaxRetries {
		c.emitEvent(Event{
			Type:      EventError,
			Error:     fmt.Errorf("max reconnection attempts reached"),
			Timestamp: time.Now(),
		})
		close(c.done)
		return
	}

	c.reconnecting = true
	c.retryCount++

	// 计算重连延迟
	delay := c.config.ReconnectConfig.Interval
	for i := 1; i < c.retryCount; i++ {
		delay = time.Duration(float64(delay) * c.config.ReconnectConfig.BackoffMultiplier)
		if delay > c.config.ReconnectConfig.MaxInterval {
			delay = c.config.ReconnectConfig.MaxInterval
			break
		}
	}

	c.emitEvent(Event{
		Type: EventReconnecting,
		Data: map[string]any{
			"attempt": c.retryCount,
			"delay":   delay,
		},
		Timestamp: time.Now(),
	})

	c.retryTimer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		if c.shouldClose {
			close(c.done)
			return
		}

		err := c.connect()
		if err != nil {
			c.scheduleReconnect()
		} else {
			c.retryCount = 0
		}
	})
}

// emitEvent 发射事件
func (c *wsClient) emitEvent(event Event) {
	c.mu.RLock()
	handlers, exists := c.handlers[event.Type]
	c.mu.RUnlock()

	if !exists {
		return
	}

	// 异步调用事件处理器
	go func() {
		for _, handler := range handlers {
			if handler != nil {
				handler(event)
			}
		}
	}()
}
