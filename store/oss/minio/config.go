package minio

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		UseSSL:         true,
		MaxConcurrency: 10,
		RequestTimeout: 30 * time.Second,
		MaxRetries:     3,
		PartSize:       5 * 1024 * 1024,        // 5MB
		MaxPartSize:    5 * 1024 * 1024 * 1024, // 5GB
		MinPartSize:    5 * 1024 * 1024,        // 5MB
		MaxParts:       10000,
		PresignExpiry:  3600 * time.Second, // 1 hour
	}
}

// Config MinIO 客户端配置
type Config struct {
	// 连接配置
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Region          string

	// 性能配置
	MaxConcurrency int           // 最大并发数
	RequestTimeout time.Duration // 请求超时时间
	MaxRetries     int           // 最大重试次数

	// 分片上传配置
	PartSize    uint64 // 默认分片大小
	MaxPartSize uint64 // 最大分片大小
	MinPartSize uint64 // 最小分片大小
	MaxParts    int    // 最大分片数

	// URL 配置
	PresignExpiry time.Duration // 预签名URL过期时间

	// 自定义 HTTP 客户端（可选）
	HTTPClient *http.Client
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return errors.New("endpoint cannot be empty")
	}
	if c.AccessKeyID == "" {
		return errors.New("access key ID cannot be empty")
	}
	if c.SecretAccessKey == "" {
		return errors.New("secret access key cannot be empty")
	}
	if c.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}
	if c.RequestTimeout <= 0 {
		return errors.New("request timeout must be greater than 0")
	}
	if c.PartSize < c.MinPartSize || c.PartSize > c.MaxPartSize {
		return fmt.Errorf("part size must be between %d and %d bytes", c.MinPartSize, c.MaxPartSize)
	}
	if c.MaxParts <= 0 || c.MaxParts > 10000 {
		return errors.New("max parts must be between 1 and 10000")
	}
	return nil
}

// Option 配置选项函数
type Option func(*Config)

// WithUseSSL 设置是否使用SSL
func WithUseSSL(useSSL bool) Option {
	return func(c *Config) {
		c.UseSSL = useSSL
	}
}

// WithRegion 设置区域
func WithRegion(region string) Option {
	return func(c *Config) {
		c.Region = region
	}
}

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(n int) Option {
	return func(c *Config) {
		c.MaxConcurrency = n
	}
}

// WithRequestTimeout 设置请求超时时间
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.RequestTimeout = timeout
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(n int) Option {
	return func(c *Config) {
		c.MaxRetries = n
	}
}

// WithPartSize 设置分片大小
func WithPartSize(size uint64) Option {
	return func(c *Config) {
		c.PartSize = size
	}
}

// WithPresignExpiry 设置预签名URL过期时间
func WithPresignExpiry(expiry time.Duration) Option {
	return func(c *Config) {
		c.PresignExpiry = expiry
	}
}

// WithHTTPClient 设置自定义HTTP客户端
func WithHTTPClient(client *http.Client) Option {
	return func(c *Config) {
		c.HTTPClient = client
	}
}
