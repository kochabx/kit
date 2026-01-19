package redis

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// Client Redis 统一客户端（支持单机/集群/哨兵模式）
type Client struct {
	client redis.UniversalClient
	config *Config

	// 日志
	logger *log.Logger

	// Hooks
	metricsHook *MetricsHook

	// 健康检查
	healthChecker *HealthChecker

	// 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool
}

// New 创建新的 Redis 客户端
// 根据配置自动选择单机/集群/哨兵模式
func New(ctx context.Context, cfg *Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// 应用选项
	clientOpts := applyOptions(cfg, opts)

	// 创建客户端上下文
	clientCtx, cancel := context.WithCancel(ctx)

	client := &Client{
		config: cfg,
		ctx:    clientCtx,
		cancel: cancel,
		logger: clientOpts.logger,
	}

	// 如果没有提供 logger，使用默认的全局 logger
	if client.logger == nil {
		client.logger = log.G
	}

	// 构建 redis.UniversalOptions
	universalOpts := client.buildUniversalOptions()

	// 创建 Redis 客户端
	client.client = redis.NewUniversalClient(universalOpts)

	// 添加 Hooks
	if err := client.setupHooks(clientOpts); err != nil {
		client.client.Close()
		cancel()
		return nil, err
	}

	// 测试连接
	if err := client.Ping(ctx); err != nil {
		client.client.Close()
		cancel()
		return nil, err
	}

	// 启动健康检查
	if clientOpts.enableHealthCheck {
		client.healthChecker = NewHealthChecker(
			client.client,
			clientOpts.healthCheckInterval,
			client.logger,
		)
		if err := client.healthChecker.Start(); err != nil {
			client.client.Close()
			cancel()
			return nil, err
		}
	}

	// 连接池预热
	if clientOpts.enablePoolWarmup {
		client.warmupPool(ctx, clientOpts.warmupSize)
	}

	client.logger.Debug().Str("mode", client.getMode()).Interface("addrs", cfg.Addrs).Msg("redis client created")

	return client, nil
}

// buildUniversalOptions 构建 redis.UniversalOptions
func (c *Client) buildUniversalOptions() *redis.UniversalOptions {
	cfg := c.config

	// 计算连接池大小
	poolSize := cfg.PoolSize
	if poolSize == 0 {
		poolSize = 10 * runtime.GOMAXPROCS(0)
	}

	opts := &redis.UniversalOptions{
		Addrs:      cfg.Addrs,
		MasterName: cfg.MasterName,

		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
		Protocol: cfg.Protocol,

		// 超时配置
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,

		// 连接池配置
		PoolSize:        poolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: cfg.MaxIdleTime,
		ConnMaxLifetime: cfg.MaxLifetime,
		PoolTimeout:     cfg.PoolTimeout,

		// 重试配置
		MaxRetries:      cfg.MaxRetries,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,

		// TLS
		TLSConfig: cfg.TLSConfig,

		// 集群特有配置
		MaxRedirects:   cfg.MaxRedirects,
		ReadOnly:       cfg.ReadOnly,
		RouteByLatency: cfg.RouteByLatency,
		RouteRandomly:  cfg.RouteRandomly,
	}

	return opts
}

// setupHooks 设置 Hooks
func (c *Client) setupHooks(opts *clientOptions) error {
	// 添加自定义 Hooks
	for _, hook := range opts.hooks {
		c.client.AddHook(hook)
	}

	// Metrics Hook
	if opts.enableMetrics {
		c.metricsHook = NewMetricsHook(
			opts.metricsNamespace,
			opts.slowQueryThresh,
		)
		c.client.AddHook(c.metricsHook)
	}

	// Slow Query Hook
	if opts.enableSlowQuery {
		slowQueryHook := NewSlowQueryHook(opts.slowQueryThresh, c.logger)
		c.client.AddHook(slowQueryHook)
	}

	// Tracing Hook
	if opts.enableTracing {
		tracingHook := NewTracingHook(opts.serviceName, c.logger)
		c.client.AddHook(tracingHook)
	}

	// Logging Hook（需要显式启用）
	if opts.enableLogging {
		loggingHook := NewLoggingHook(c.logger)
		c.client.AddHook(loggingHook)
	}

	return nil
}

// warmupPool 预热连接池
func (c *Client) warmupPool(ctx context.Context, size int) {
	if size == 0 {
		size = c.config.MinIdleConns
	}

	if size <= 0 {
		return
	}

	c.logger.Debug().Int("size", size).Msg("warming up connection pool")

	// 并发创建连接
	for i := 0; i < size; i++ {
		go func() {
			_ = c.client.Ping(ctx).Err()
		}()
	}

	// 等待一小段时间让连接建立
	time.Sleep(100 * time.Millisecond)
}

// UniversalClient 获取底层 redis.UniversalClient
// 用于执行所有 Redis 命令
func (c *Client) UniversalClient() redis.UniversalClient {
	if c.closed.Load() {
		return nil
	}
	return c.client
}

// Ping 测试连接
func (c *Client) Ping(ctx context.Context) error {
	if c.closed.Load() {
		return ErrClientClosed
	}

	return c.client.Ping(ctx).Err()
}

// Close 关闭客户端
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil // 已经关闭
	}

	c.logger.Debug().Msg("closing redis client")

	// 停止健康检查
	if c.healthChecker != nil {
		_ = c.healthChecker.Stop()
	}

	// 关闭客户端
	err := c.client.Close()

	// 取消上下文
	c.cancel()

	c.logger.Debug().Msg("redis client closed")

	return err
}

// Stats 获取连接池统计信息
func (c *Client) Stats() *redis.PoolStats {
	if c.closed.Load() {
		return nil
	}

	return c.client.PoolStats()
}

// HealthCheck 执行健康检查
func (c *Client) HealthCheck(ctx context.Context) error {
	if c.closed.Load() {
		return ErrClientClosed
	}

	// 执行 PING
	if err := c.Ping(ctx); err != nil {
		return err
	}

	// 检查连接池状态
	stats := c.Stats()
	if stats == nil {
		return ErrHealthCheckFailed
	}

	// 检查是否有太多超时
	if stats.Timeouts > 0 && stats.Hits > 0 {
		timeoutRate := float64(stats.Timeouts) / float64(stats.Hits)
		if timeoutRate > 0.1 { // 超时率超过 10%
			c.logger.Warn().Uint32("timeouts", stats.Timeouts).Uint32("hits", stats.Hits).Float64("rate", timeoutRate).Msg("high timeout rate detected")
		}
	}

	return nil
}

// GetHealthStatus 获取健康状态
func (c *Client) GetHealthStatus() *HealthStatus {
	if c.healthChecker == nil {
		// 如果没有启用健康检查，手动检查一次
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		start := time.Now()
		err := c.Ping(ctx)
		latency := time.Since(start)

		status := &HealthStatus{
			LastCheck: time.Now(),
			Latency:   latency,
			PoolStats: c.Stats(),
		}

		if err != nil {
			status.Healthy = false
			status.ErrorMessage = err.Error()
		} else {
			status.Healthy = true
		}

		return status
	}

	return c.healthChecker.GetStatus()
}

// GetMetrics 获取 Metrics 统计
func (c *Client) GetMetrics() *Metrics {
	if c.metricsHook == nil {
		return nil
	}

	return c.metricsHook.GetMetrics()
}

// IsClosed 检查客户端是否已关闭
func (c *Client) IsClosed() bool {
	return c.closed.Load()
}

// getMode 获取客户端模式
func (c *Client) getMode() string {
	if c.config.IsSentinel() {
		return "sentinel"
	}
	if c.config.IsCluster() {
		return "cluster"
	}
	return "single"
}
