package redis

import (
	"context"
	"runtime"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// Client Redis 统一客户端（支持单机/集群/哨兵模式）
type Client struct {
	client redis.UniversalClient
	config *Config

	// 日志
	logger *log.Logger
}

// New 创建新的 Redis 客户端
// 根据配置自动选择单机/集群/哨兵模式
func New(cfg *Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}

	if err := cfg.ApplyDefaults(); err != nil {
		return nil, err
	}

	clientOpts := applyOptions(cfg, opts)
	logger := clientOpts.logger
	if logger == nil {
		logger = log.G
	}

	client := &Client{
		config: cfg,
		logger: logger,
		client: redis.NewUniversalClient(buildUniversalOptions(cfg)),
	}

	// 错误时自动清理
	var success bool
	defer func() {
		if !success {
			client.client.Close()
		}
	}()

	if err := client.setupHooks(clientOpts); err != nil {
		return nil, err
	}
	if err := client.Ping(context.Background()); err != nil {
		return nil, err
	}

	success = true
	client.logger.Debug().Str("mode", client.getMode()).Interface("addrs", cfg.Addrs).Msg("redis client created")
	return client, nil
}

// buildUniversalOptions 构建 redis.UniversalOptions
func buildUniversalOptions(cfg *Config) *redis.UniversalOptions {
	poolSize := cfg.PoolSize
	if poolSize == 0 {
		poolSize = 10 * runtime.GOMAXPROCS(0)
	}

	return &redis.UniversalOptions{
		Addrs:      cfg.Addrs,
		MasterName: cfg.MasterName,
		Username:   cfg.Username,
		Password:   cfg.Password,
		DB:         cfg.DB,
		Protocol:   cfg.Protocol,

		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,

		PoolSize:        poolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: cfg.MaxIdleTime,
		ConnMaxLifetime: cfg.MaxLifetime,
		PoolTimeout:     cfg.PoolTimeout,

		MaxRetries:      cfg.MaxRetries,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,

		TLSConfig:      cfg.TLSConfig,
		MaxRedirects:   cfg.MaxRedirects,
		ReadOnly:       cfg.ReadOnly,
		RouteByLatency: cfg.RouteByLatency,
		RouteRandomly:  cfg.RouteRandomly,
	}
}

// setupHooks 设置 Hooks
func (c *Client) setupHooks(opts *clientOptions) error {
	// 添加自定义 Hooks
	for _, hook := range opts.hooks {
		c.client.AddHook(hook)
	}

	// OpenTelemetry Tracing Hook
	if opts.enableTracing {
		if err := redisotel.InstrumentTracing(c.client, opts.tracingOpts...); err != nil {
			return err
		}
	}

	// OpenTelemetry Metrics Hook
	if opts.enableMetrics {
		if err := redisotel.InstrumentMetrics(c.client, opts.metricsOpts...); err != nil {
			return err
		}
	}

	// Debug Hook
	if opts.enableDebug {
		debugHook := NewDebugHook(c.logger, opts.slowQueryThresh)
		c.client.AddHook(debugHook)
	}

	return nil
}

// UniversalClient 获取底层 redis.UniversalClient
// 用于执行所有 Redis 命令
func (c *Client) UniversalClient() redis.UniversalClient {
	return c.client
}

// Ping 测试连接
func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close 关闭客户端
func (c *Client) Close() error {
	err := c.client.Close()
	c.logger.Debug().Msg("redis client closed")
	return err
}

// Stats 获取连接池统计信息
func (c *Client) Stats() *redis.PoolStats {
	return c.client.PoolStats()
}

// getMode 获取客户端模式
func (c *Client) getMode() string {
	switch {
	case c.config.IsSentinel():
		return "sentinel"
	case c.config.IsCluster():
		return "cluster"
	default:
		return "single"
	}
}
