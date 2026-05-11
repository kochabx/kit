package wsx

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Config WebSocket 客户端配置。
type Config struct {
	// HandshakeTimeout 握手超时
	HandshakeTimeout time.Duration `json:"handshake_timeout" yaml:"handshake_timeout"`
	// ReadTimeout 单次读取超时
	ReadTimeout time.Duration `json:"read_timeout" yaml:"read_timeout"`
	// WriteTimeout 单次写入超时
	WriteTimeout time.Duration `json:"write_timeout" yaml:"write_timeout"`
	// PingInterval 心跳间隔，<=0 关闭主动 ping
	PingInterval time.Duration `json:"ping_interval" yaml:"ping_interval"`
	// PongWait 等待 pong 的最大时长
	PongWait time.Duration `json:"pong_wait" yaml:"pong_wait"`
	// MaxMessageSize 单条消息最大字节数
	MaxMessageSize int64 `json:"max_message_size" yaml:"max_message_size"`
	// ReadBufferSize 读缓冲区大小
	ReadBufferSize int `json:"read_buffer_size" yaml:"read_buffer_size"`
	// WriteBufferSize 写缓冲区大小
	WriteBufferSize int `json:"write_buffer_size" yaml:"write_buffer_size"`
	// WriteQueueSize 写队列长度
	WriteQueueSize int `json:"write_queue_size" yaml:"write_queue_size"`
	// EnableCompression 是否启用压缩
	EnableCompression bool `json:"enable_compression" yaml:"enable_compression"`
	// Reconnect 自动重连配置
	Reconnect ReconnectConfig `json:"reconnect" yaml:"reconnect"`
}

// ReconnectConfig 自动重连配置。
type ReconnectConfig struct {
	// Enable 是否启用自动重连
	Enable bool `json:"enable" yaml:"enable"`
	// MaxRetries 最大重连次数；0 表示无限制
	MaxRetries int `json:"max_retries" yaml:"max_retries"`
	// Interval 初次重连间隔
	Interval time.Duration `json:"interval" yaml:"interval"`
	// MaxInterval 重连间隔上限
	MaxInterval time.Duration `json:"max_interval" yaml:"max_interval"`
	// BackoffMultiplier 退避倍数
	BackoffMultiplier float64 `json:"backoff_multiplier" yaml:"backoff_multiplier"`
}

// DefaultConfig 返回一份默认配置。
func DefaultConfig() Config {
	return Config{
		HandshakeTimeout:  30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingInterval:      54 * time.Second,
		PongWait:          60 * time.Second,
		MaxMessageSize:    1024 * 1024,
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		WriteQueueSize:    128,
		EnableCompression: false,
		Reconnect: ReconnectConfig{
			Enable:            true,
			MaxRetries:        5,
			Interval:          time.Second,
			MaxInterval:       30 * time.Second,
			BackoffMultiplier: 2.0,
		},
	}
}

// Option 配置 Client 的可选项。
type Option func(*Client)

// WithConfig 一次性替换全部配置。
func WithConfig(cfg Config) Option {
	return func(c *Client) { c.config = cfg }
}

// WithHeaders 设置握手时附加的 HTTP 头。
func WithHeaders(headers http.Header) Option {
	return func(c *Client) { c.headers = headers }
}

// WithDialer 直接注入自定义的 websocket.Dialer，将覆盖默认 Dialer。
func WithDialer(d *websocket.Dialer) Option {
	return func(c *Client) { c.dialer = d }
}

// WithTLSConfig 设置 TLS 配置；在 wss:// 场景下生效。
func WithTLSConfig(t *tls.Config) Option {
	return func(c *Client) { c.tlsConfig = t }
}

// WithHandshakeTimeout 设置握手超时。
func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *Client) { c.config.HandshakeTimeout = d }
}

// WithReadTimeout 设置读取超时。
func WithReadTimeout(d time.Duration) Option {
	return func(c *Client) { c.config.ReadTimeout = d }
}

// WithWriteTimeout 设置写入超时。
func WithWriteTimeout(d time.Duration) Option {
	return func(c *Client) { c.config.WriteTimeout = d }
}

// WithPingInterval 设置心跳间隔；<=0 关闭主动 ping。
func WithPingInterval(d time.Duration) Option {
	return func(c *Client) { c.config.PingInterval = d }
}

// WithPongWait 设置等待 pong 的最大时长。
func WithPongWait(d time.Duration) Option {
	return func(c *Client) { c.config.PongWait = d }
}

// WithMaxMessageSize 设置最大消息字节数。
func WithMaxMessageSize(size int64) Option {
	return func(c *Client) { c.config.MaxMessageSize = size }
}

// WithReadBufferSize 设置读缓冲区大小。
func WithReadBufferSize(size int) Option {
	return func(c *Client) { c.config.ReadBufferSize = size }
}

// WithWriteBufferSize 设置写缓冲区大小。
func WithWriteBufferSize(size int) Option {
	return func(c *Client) { c.config.WriteBufferSize = size }
}

// WithWriteQueueSize 设置写队列长度。
func WithWriteQueueSize(size int) Option {
	return func(c *Client) { c.config.WriteQueueSize = size }
}

// WithEnableCompression 启用/关闭压缩。
func WithEnableCompression(enable bool) Option {
	return func(c *Client) { c.config.EnableCompression = enable }
}

// WithReconnect 设置自动重连策略。
func WithReconnect(rc ReconnectConfig) Option {
	return func(c *Client) { c.config.Reconnect = rc }
}
