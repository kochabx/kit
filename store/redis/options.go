package redis

import (
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// Option 客户端配置选项
type Option func(*clientOptions)

// clientOptions 客户端内部选项
type clientOptions struct {
	// 基础配置覆盖
	password string
	username string
	db       int
	poolSize int

	// Hooks
	hooks []redis.Hook

	// 可观测性
	enableMetrics bool
	enableTracing bool
	enableDebug   bool
	tracingOpts   []redisotel.TracingOption
	metricsOpts   []redisotel.MetricsOption

	// 日志
	logger          *log.Logger
	slowQueryThresh time.Duration // 慢查询阈值，用于 DebugHook
}

// ==================== 基础配置选项 ====================

// WithPassword 设置密码
func WithPassword(password string) Option {
	return func(o *clientOptions) {
		o.password = password
	}
}

// WithUsername 设置用户名 (Redis 6.0+)
func WithUsername(username string) Option {
	return func(o *clientOptions) {
		o.username = username
	}
}

// WithDB 设置数据库索引（仅单机和哨兵模式有效）
func WithDB(db int) Option {
	return func(o *clientOptions) {
		o.db = db
	}
}

// WithPoolSize 设置连接池大小
func WithPoolSize(size int) Option {
	return func(o *clientOptions) {
		o.poolSize = size
	}
}

// ==================== Hooks 选项 ====================

// WithHooks 添加自定义 Hooks
func WithHooks(hooks ...redis.Hook) Option {
	return func(o *clientOptions) {
		o.hooks = append(o.hooks, hooks...)
	}
}

// ==================== 可观测性选项 ====================

// WithMetrics 启用 OpenTelemetry Metrics 收集
// 使用 redisotel 官方实现，指标会通过 OpenTelemetry 导出
func WithMetrics(opts ...redisotel.MetricsOption) Option {
	return func(o *clientOptions) {
		o.enableMetrics = true
		o.metricsOpts = opts
	}
}

// WithTracing 启用 OpenTelemetry 分布式追踪
// 使用 redisotel 官方实现
func WithTracing(opts ...redisotel.TracingOption) Option {
	return func(o *clientOptions) {
		o.enableTracing = true
		o.tracingOpts = opts
	}
}

// WithDebug 启用调试模式（日志记录 + 慢查询检测）
// 注意：这会记录每个 Redis 命令的执行详情，可能产生大量日志
// slowQueryThreshold: 慢查询阈值，超过此时间的查询会记录为警告，0 表示不检测慢查询
func WithDebug(slowQueryThreshold ...time.Duration) Option {
	return func(o *clientOptions) {
		o.enableDebug = true
		if len(slowQueryThreshold) > 0 {
			o.slowQueryThresh = slowQueryThreshold[0]
		}
	}
}

// WithLogger 设置日志记录器
// logger 用于记录客户端生命周期、健康检查、慢查询等信息
// 如果需要记录每个命令的详细日志，请同时使用 WithLogging()
func WithLogger(logger *log.Logger) Option {
	return func(o *clientOptions) {
		o.logger = logger
	}
}

// ==================== 内部辅助函数 ====================

// applyOptions 应用所有选项到配置
func applyOptions(cfg *Config, opts []Option) *clientOptions {
	clientOpts := &clientOptions{}

	// 应用所有选项
	for _, opt := range opts {
		if opt != nil {
			opt(clientOpts)
		}
	}

	// 将选项值应用到配置
	if clientOpts.password != "" {
		cfg.Password = clientOpts.password
	}
	if clientOpts.username != "" {
		cfg.Username = clientOpts.username
	}
	if clientOpts.db > 0 {
		cfg.DB = clientOpts.db
	}
	if clientOpts.poolSize > 0 {
		cfg.PoolSize = clientOpts.poolSize
	}

	return clientOpts
}
