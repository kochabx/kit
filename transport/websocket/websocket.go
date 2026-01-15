package websocket

import (
	"context"
	"net/http"
	"time"
)

// MessageType 定义消息类型
type MessageType int

const (
	// TextMessage 文本消息
	TextMessage MessageType = 1
	// BinaryMessage 二进制消息
	BinaryMessage MessageType = 2
	// CloseMessage 关闭消息
	CloseMessage MessageType = 8
	// PingMessage ping消息
	PingMessage MessageType = 9
	// PongMessage pong消息
	PongMessage MessageType = 10
)

// Message WebSocket消息结构
type Message struct {
	Type MessageType `json:"type"`
	Data []byte      `json:"data"`
}

// Config WebSocket客户端配置
type Config struct {
	// 连接超时时间
	ConnectTimeout time.Duration `json:"connect_timeout" yaml:"connect_timeout"`
	// 读超时时间
	ReadTimeout time.Duration `json:"read_timeout" yaml:"read_timeout"`
	// 写超时时间
	WriteTimeout time.Duration `json:"write_timeout" yaml:"write_timeout"`
	// ping间隔时间
	PingInterval time.Duration `json:"ping_interval" yaml:"ping_interval"`
	// pong等待时间
	PongWait time.Duration `json:"pong_wait" yaml:"pong_wait"`
	// 最大消息大小
	MaxMessageSize int64 `json:"max_message_size" yaml:"max_message_size"`
	// 读缓冲区大小
	ReadBufferSize int `json:"read_buffer_size" yaml:"read_buffer_size"`
	// 写缓冲区大小
	WriteBufferSize int `json:"write_buffer_size" yaml:"write_buffer_size"`
	// 是否启用压缩
	EnableCompression bool `json:"enable_compression" yaml:"enable_compression"`
	// 重连配置
	ReconnectConfig ReconnectConfig `json:"reconnect_config" yaml:"reconnect_config"`
}

// ReconnectConfig 重连配置
type ReconnectConfig struct {
	// 是否启用自动重连
	Enable bool `json:"enable" yaml:"enable"`
	// 最大重连次数，0表示无限制
	MaxRetries int `json:"max_retries" yaml:"max_retries"`
	// 重连间隔时间
	Interval time.Duration `json:"interval" yaml:"interval"`
	// 最大重连间隔时间
	MaxInterval time.Duration `json:"max_interval" yaml:"max_interval"`
	// 退避倍数
	BackoffMultiplier float64 `json:"backoff_multiplier" yaml:"backoff_multiplier"`
}

type wsClientOption func(*wsClient)

func WithHeaders(headers http.Header) wsClientOption {
	return func(c *wsClient) {
		c.headers = headers
	}
}

func WithConnectTimeout(d time.Duration) wsClientOption {
	return func(c *wsClient) {
		c.config.ConnectTimeout = d
	}
}

func WithReadTimeout(d time.Duration) wsClientOption {
	return func(c *wsClient) {
		c.config.ReadTimeout = d
	}
}

func WithWriteTimeout(d time.Duration) wsClientOption {
	return func(c *wsClient) {
		c.config.WriteTimeout = d
	}
}

func WithPingInterval(d time.Duration) wsClientOption {
	return func(c *wsClient) {
		c.config.PingInterval = d
	}
}

func WithPongWait(d time.Duration) wsClientOption {
	return func(c *wsClient) {
		c.config.PongWait = d
	}
}

func WithMaxMessageSize(size int64) wsClientOption {
	return func(c *wsClient) {
		c.config.MaxMessageSize = size
	}
}

func WithReadBufferSize(size int) wsClientOption {
	return func(c *wsClient) {
		c.config.ReadBufferSize = size
	}
}

func WithWriteBufferSize(size int) wsClientOption {
	return func(c *wsClient) {
		c.config.WriteBufferSize = size
	}
}

func WithEnableCompression(enable bool) wsClientOption {
	return func(c *wsClient) {
		c.config.EnableCompression = enable
	}
}

func WithReconnectConfig(reconnectConfig ReconnectConfig) wsClientOption {
	return func(c *wsClient) {
		c.config.ReconnectConfig = reconnectConfig
	}
}

// defaultConfig 默认配置
func defaultConfig() Config {
	return Config{
		ConnectTimeout:    30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingInterval:      54 * time.Second,
		PongWait:          60 * time.Second,
		MaxMessageSize:    1024 * 1024, // 1MB
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		EnableCompression: false,
		ReconnectConfig: ReconnectConfig{
			Enable:            true,
			MaxRetries:        5,
			Interval:          1 * time.Second,
			MaxInterval:       30 * time.Second,
			BackoffMultiplier: 2.0,
		},
	}
}

// EventType 事件类型
type EventType string

const (
	// EventConnected 连接成功事件
	EventConnected EventType = "connected"
	// EventDisconnected 连接断开事件
	EventDisconnected EventType = "disconnected"
	// EventMessage 收到消息事件
	EventMessage EventType = "message"
	// EventError 错误事件
	EventError EventType = "error"
	// EventReconnecting 重连中事件
	EventReconnecting EventType = "reconnecting"
)

// Event WebSocket事件
type Event struct {
	Type      EventType `json:"type"`
	Data      any       `json:"data,omitempty"`
	Error     error     `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler 事件处理器
type EventHandler func(event Event)

// Client WebSocket客户端接口
type Client interface {
	// Connect 连接到WebSocket服务器
	Connect(ctx context.Context, url string) error
	// Disconnect 断开连接
	Disconnect() error
	// Send 发送消息
	Send(messageType MessageType, data []byte) error
	// SendText 发送文本消息
	SendText(text string) error
	// SendBinary 发送二进制消息
	SendBinary(data []byte) error
	// OnEvent 注册事件处理器
	OnEvent(eventType EventType, handler EventHandler)
	// RemoveEventHandler 移除事件处理器
	RemoveEventHandler(eventType EventType)
	// IsConnected 检查是否已连接
	IsConnected() bool
	// GetConfig 获取配置
	GetConfig() Config
	// Close 关闭客户端
	Close() error
}
