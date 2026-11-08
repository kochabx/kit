package wsx

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Config configures a WebSocket client.
type Config struct {
	// HandshakeTimeout limits handshake duration.
	HandshakeTimeout time.Duration `json:"handshake_timeout" yaml:"handshake_timeout"`
	// ReadTimeout configures the read timeout.
	ReadTimeout time.Duration `json:"read_timeout" yaml:"read_timeout"`
	// WriteTimeout limits a write operation.
	WriteTimeout time.Duration `json:"write_timeout" yaml:"write_timeout"`
	// PingInterval controls active pings; values <= 0 disable them.
	PingInterval time.Duration `json:"ping_interval" yaml:"ping_interval"`
	// PongWait limits the wait for a pong response.
	PongWait time.Duration `json:"pong_wait" yaml:"pong_wait"`
	// MaxMessageSize limits the size of one message in bytes.
	MaxMessageSize int64 `json:"max_message_size" yaml:"max_message_size"`
	// ReadBufferSize sets the read buffer size.
	ReadBufferSize int `json:"read_buffer_size" yaml:"read_buffer_size"`
	// WriteBufferSize sets the write buffer size.
	WriteBufferSize int `json:"write_buffer_size" yaml:"write_buffer_size"`
	// WriteQueueSize sets the send queue capacity.
	WriteQueueSize int `json:"write_queue_size" yaml:"write_queue_size"`
	// EnableCompression enables message compression.
	EnableCompression bool `json:"enable_compression" yaml:"enable_compression"`
	// Reconnect configures automatic reconnection.
	Reconnect ReconnectConfig `json:"reconnect" yaml:"reconnect"`
}

// ReconnectConfig configures automatic reconnection.
type ReconnectConfig struct {
	// Enable enables automatic reconnection.
	Enable bool `json:"enable" yaml:"enable"`
	// MaxRetries limits reconnect attempts; zero means unlimited attempts.
	MaxRetries int `json:"max_retries" yaml:"max_retries"`
	// Interval is the initial reconnect delay.
	Interval time.Duration `json:"interval" yaml:"interval"`
	// MaxInterval caps the reconnect delay.
	MaxInterval time.Duration `json:"max_interval" yaml:"max_interval"`
	// BackoffMultiplier is the exponential backoff factor.
	BackoffMultiplier float64 `json:"backoff_multiplier" yaml:"backoff_multiplier"`
}

// DefaultConfig returns a fresh default configuration.
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

// Option configures a Client.
type Option func(*Client)

// WithConfig replaces the entire configuration.
func WithConfig(cfg Config) Option {
	return func(c *Client) { c.config = cfg }
}

// WithHeaders sets additional handshake headers.
func WithHeaders(headers http.Header) Option {
	return func(c *Client) { c.headers = headers }
}

// WithDialer supplies a custom websocket.Dialer instead of the default dialer.
func WithDialer(d *websocket.Dialer) Option {
	return func(c *Client) { c.dialer = d }
}

// WithTLSConfig sets the TLS configuration for wss connections.
func WithTLSConfig(t *tls.Config) Option {
	return func(c *Client) { c.tlsConfig = t }
}

// WithHandshakeTimeout limits handshake duration.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *Client) { c.config.HandshakeTimeout = d }
}

// WithReadTimeout configures the read timeout.
func WithReadTimeout(d time.Duration) Option {
	return func(c *Client) { c.config.ReadTimeout = d }
}

// WithWriteTimeout limits write duration.
func WithWriteTimeout(d time.Duration) Option {
	return func(c *Client) { c.config.WriteTimeout = d }
}

// WithPingInterval sets the heartbeat interval; values <= 0 disable active pings.
func WithPingInterval(d time.Duration) Option {
	return func(c *Client) { c.config.PingInterval = d }
}

// WithPongWait limits the wait for a pong response.
func WithPongWait(d time.Duration) Option {
	return func(c *Client) { c.config.PongWait = d }
}

// WithMaxMessageSize limits one message's size in bytes.
func WithMaxMessageSize(size int64) Option {
	return func(c *Client) { c.config.MaxMessageSize = size }
}

// WithReadBufferSize sets the read buffer size.
func WithReadBufferSize(size int) Option {
	return func(c *Client) { c.config.ReadBufferSize = size }
}

// WithWriteBufferSize sets the write buffer size.
func WithWriteBufferSize(size int) Option {
	return func(c *Client) { c.config.WriteBufferSize = size }
}

// WithWriteQueueSize sets the send queue capacity.
func WithWriteQueueSize(size int) Option {
	return func(c *Client) { c.config.WriteQueueSize = size }
}

// WithEnableCompression enables or disables compression.
func WithEnableCompression(enable bool) Option {
	return func(c *Client) { c.config.EnableCompression = enable }
}

// WithReconnect sets the automatic reconnect policy.
func WithReconnect(rc ReconnectConfig) Option {
	return func(c *Client) { c.config.Reconnect = rc }
}
