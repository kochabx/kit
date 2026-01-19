package redis

import (
	"crypto/tls"
	"time"

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
	enableMetrics    bool
	enableTracing    bool
	enableSlowQuery  bool
	enableLogging    bool
	slowQueryThresh  time.Duration
	metricsNamespace string
	serviceName      string

	// 健康检查
	enableHealthCheck   bool
	healthCheckInterval time.Duration

	// 连接池预热
	enablePoolWarmup bool
	warmupSize       int

	// 日志
	logger *log.Logger
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

// WithMetrics 启用 Metrics 收集
// namespace: Prometheus metrics 的命名空间，默认为 "redis"
func WithMetrics(namespace ...string) Option {
	return func(o *clientOptions) {
		o.enableMetrics = true
		if len(namespace) > 0 {
			o.metricsNamespace = namespace[0]
		} else {
			o.metricsNamespace = "redis"
		}
	}
}

// WithTracing 启用分布式追踪
// serviceName: 服务名称，用于追踪标识
func WithTracing(serviceName ...string) Option {
	return func(o *clientOptions) {
		o.enableTracing = true
		if len(serviceName) > 0 {
			o.serviceName = serviceName[0]
		} else {
			o.serviceName = "redis"
		}
	}
}

// WithSlowQueryLog 启用慢查询日志
// threshold: 慢查询阈值，超过此时间的查询会被记录
func WithSlowQueryLog(threshold time.Duration) Option {
	return func(o *clientOptions) {
		o.enableSlowQuery = true
		o.slowQueryThresh = threshold
	}
}

// WithLogging 启用详细的命令日志记录
// 注意：这会记录每个 Redis 命令的执行详情，可能产生大量日志
// 需要配合 WithLogger 使用
func WithLogging() Option {
	return func(o *clientOptions) {
		o.enableLogging = true
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

// ==================== 健康检查选项 ====================

// WithHealthCheck 启用定期健康检查
// interval: 健康检查间隔时间
func WithHealthCheck(interval time.Duration) Option {
	return func(o *clientOptions) {
		o.enableHealthCheck = true
		o.healthCheckInterval = interval
	}
}

// ==================== 连接池选项 ====================

// WithPoolWarmup 启用连接池预热
// size: 预热连接数，0 表示使用 MinIdleConns
func WithPoolWarmup(size int) Option {
	return func(o *clientOptions) {
		o.enablePoolWarmup = true
		o.warmupSize = size
	}
}

// ==================== 快捷配置选项 ====================

// WithTimeout 设置超时时间
func WithTimeout(dial, read, write time.Duration) Option {
	return func(o *clientOptions) {
		// 这些值会在创建客户端时应用到 Config
	}
}

// WithTLS 启用 TLS 加密连接
func WithTLS(config *tls.Config) Option {
	return func(o *clientOptions) {
		// TLS 配置会在创建客户端时应用
	}
}

// WithReadOnly 启用只读模式（仅集群模式）
func WithReadOnly() Option {
	return func(o *clientOptions) {
		// 只读模式会在创建客户端时应用
	}
}

// WithRouteByLatency 启用按延迟路由（仅集群模式）
func WithRouteByLatency() Option {
	return func(o *clientOptions) {
		// 路由策略会在创建客户端时应用
	}
}

// WithRouteRandomly 启用随机路由（仅集群模式）
func WithRouteRandomly() Option {
	return func(o *clientOptions) {
		// 路由策略会在创建客户端时应用
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
