package scheduler

import (
	"time"

	"github.com/kochabx/kit/log"
	"github.com/redis/go-redis/v9"
)

// RedisOptions Redis配置
type RedisOptions struct {
	Client       *redis.Client // Redis客户端（可选，如果不提供则使用Addr创建）
	Addr         string        // Redis地址
	DB           int           // Redis数据库
	Password     string        // Redis密码
	PoolSize     int           // 连接池大小（默认：CPU核心数*10）
	MinIdleConns int           // 最小空闲连接数（默认：5）
	MaxRetries   int           // 最大重试次数（默认：3）
	DialTimeout  time.Duration // 连接超时（默认：5秒）
	ReadTimeout  time.Duration // 读取超时（默认：3秒）
	WriteTimeout time.Duration // 写入超时（默认：3秒）
	PoolTimeout  time.Duration // 连接池超时（默认：4秒）
}

// WorkerOptions Worker配置
type WorkerOptions struct {
	Count               int           // Worker数量
	Concurrency         int           // 每个Worker的并发协程数（默认：5）
	LeaseTTL            time.Duration // Worker租约TTL
	RenewInterval       time.Duration // Worker续约间隔
	ShutdownGracePeriod time.Duration // 优雅关闭等待时间
}

// RetryOptions 重试配置
type RetryOptions struct {
	MaxRetry   int           // 默认最大重试次数
	BaseDelay  time.Duration // 重试基础延迟
	MaxDelay   time.Duration // 重试最大延迟
	Multiplier float64       // 重试指数乘数
	Jitter     bool          // 是否启用重试随机抖动
}

// RateLimitOptions 限流配置
type RateLimitOptions struct {
	Enabled bool // 是否启用限流
	Rate    int  // 每秒允许的任务数
	Burst   int  // 突发容量
}

// CircuitBreakerOptions 熔断配置
type CircuitBreakerOptions struct {
	Enabled     bool          // 是否启用熔断
	MaxFailures int           // 最大失败次数
	Timeout     time.Duration // 熔断超时
}

// MetricsOptions 监控配置
type MetricsOptions struct {
	Enabled bool   // 是否启用Prometheus指标
	Port    int    // 指标HTTP端口
	Path    string // 指标路径
}

// HealthOptions 健康检查配置
type HealthOptions struct {
	Enabled bool   // 是否启用健康检查
	Port    int    // 健康检查HTTP端口
	Path    string // 健康检查路径
}

// Options 调度器配置选项
type Options struct {
	// 基础配置
	Namespace string // 命名空间

	// Redis配置
	Redis RedisOptions

	// Worker配置
	Worker WorkerOptions

	// 队列配置
	ScanInterval time.Duration // 扫描延迟队列的间隔
	BatchSize    int           // 批量移动任务的数量

	// 锁配置
	LockTimeout time.Duration // 分布式锁超时时间

	// 重试配置
	Retry RetryOptions

	// 去重配置
	DedupEnabled    bool          // 是否启用去重
	DedupDefaultTTL time.Duration // 默认去重TTL

	// 死信队列配置
	DLQEnabled bool // 是否启用死信队列
	DLQMaxSize int  // 死信队列最大容量

	// 限流配置
	RateLimit RateLimitOptions

	// 熔断配置
	CircuitBreaker CircuitBreakerOptions

	// 监控配置
	Metrics MetricsOptions

	// 健康检查配置
	Health HealthOptions

	// 日志配置
	CustomLogger *log.Logger // 自定义日志记录器（可选，默认使用 log.L）
}

// DefaultOptions 返回默认配置
func DefaultOptions() *Options {
	return &Options{
		Namespace: "scheduler",
		Redis: RedisOptions{
			Addr:         "localhost:6379",
			DB:           0,
			PoolSize:     0, // 0 表示使用默认值（CPU核心数*10）
			MinIdleConns: 5,
			MaxRetries:   3,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			PoolTimeout:  4 * time.Second,
		},
		Worker: WorkerOptions{
			Count:               10,
			Concurrency:         5,
			LeaseTTL:            30 * time.Second,
			RenewInterval:       10 * time.Second,
			ShutdownGracePeriod: 30 * time.Second,
		},
		ScanInterval: 1 * time.Second,
		BatchSize:    100,
		LockTimeout:  5 * time.Minute,
		Retry: RetryOptions{
			MaxRetry:   3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   1 * time.Hour,
			Multiplier: 2.0,
			Jitter:     true,
		},
		DedupEnabled:    true,
		DedupDefaultTTL: 1 * time.Hour,
		DLQEnabled:      true,
		DLQMaxSize:      10000,
		RateLimit: RateLimitOptions{
			Enabled: false,
			Rate:    1000,
			Burst:   2000,
		},
		CircuitBreaker: CircuitBreakerOptions{
			Enabled:     false,
			MaxFailures: 5,
			Timeout:     30 * time.Second,
		},
		Metrics: MetricsOptions{
			Enabled: false,
			Port:    9090,
			Path:    "/metrics",
		},
		Health: HealthOptions{
			Enabled: false,
			Port:    8080,
			Path:    "/health",
		},
	}
}

// Option 函数式选项
type Option func(*Options)

// WithNamespace 设置命名空间
func WithNamespace(namespace string) Option {
	return func(o *Options) {
		o.Namespace = namespace
	}
}

// WithRedisClient 使用现有Redis客户端
func WithRedisClient(client *redis.Client) Option {
	return func(o *Options) {
		o.Redis.Client = client
	}
}

// WithRedisAddr 设置Redis地址
func WithRedisAddr(addr string) Option {
	return func(o *Options) {
		o.Redis.Addr = addr
	}
}

// WithRedisDB 设置Redis数据库
func WithRedisDB(db int) Option {
	return func(o *Options) {
		o.Redis.DB = db
	}
}

// WithRedisPass 设置Redis密码
func WithRedisPass(pass string) Option {
	return func(o *Options) {
		o.Redis.Password = pass
	}
}

// WithWorkerCount 设置Worker数量
func WithWorkerCount(count int) Option {
	return func(o *Options) {
		o.Worker.Count = count
	}
}

// WithWorkerConcurrency 设置每个Worker的并发协程数
func WithWorkerConcurrency(concurrency int) Option {
	return func(o *Options) {
		o.Worker.Concurrency = concurrency
	}
}

// WithLeaseTTL 设置Worker租约TTL
func WithLeaseTTL(ttl time.Duration) Option {
	return func(o *Options) {
		o.Worker.LeaseTTL = ttl
	}
}

// WithScanInterval 设置扫描间隔
func WithScanInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.ScanInterval = interval
	}
}

// WithBatchSize 设置批量处理大小
func WithBatchSize(size int) Option {
	return func(o *Options) {
		o.BatchSize = size
	}
}

// WithMaxRetry 设置最大重试次数
func WithMaxRetry(maxRetry int) Option {
	return func(o *Options) {
		o.Retry.MaxRetry = maxRetry
	}
}

// WithRetryStrategy 设置重试策略参数
func WithRetryStrategy(baseDelay, maxDelay time.Duration, multiplier float64, jitter bool) Option {
	return func(o *Options) {
		o.Retry.BaseDelay = baseDelay
		o.Retry.MaxDelay = maxDelay
		o.Retry.Multiplier = multiplier
		o.Retry.Jitter = jitter
	}
}

// WithDeduplication 启用/禁用去重
func WithDeduplication(enabled bool, defaultTTL time.Duration) Option {
	return func(o *Options) {
		o.DedupEnabled = enabled
		o.DedupDefaultTTL = defaultTTL
	}
}

// WithDLQ 启用/禁用死信队列
func WithDLQ(enabled bool, maxSize int) Option {
	return func(o *Options) {
		o.DLQEnabled = enabled
		o.DLQMaxSize = maxSize
	}
}

// WithRateLimit 启用限流
func WithRateLimit(enabled bool, rate, burst int) Option {
	return func(o *Options) {
		o.RateLimit.Enabled = enabled
		o.RateLimit.Rate = rate
		o.RateLimit.Burst = burst
	}
}

// WithCircuitBreaker 启用熔断
func WithCircuitBreaker(enabled bool, maxFailures int, timeout time.Duration) Option {
	return func(o *Options) {
		o.CircuitBreaker.Enabled = enabled
		o.CircuitBreaker.MaxFailures = maxFailures
		o.CircuitBreaker.Timeout = timeout
	}
}

// WithMetrics 启用Prometheus指标
func WithMetrics(enabled bool) Option {
	return func(o *Options) {
		o.Metrics.Enabled = enabled
	}
}

// WithMetricsPort 设置指标端口
func WithMetricsPort(port int) Option {
	return func(o *Options) {
		o.Metrics.Port = port
	}
}

// WithHealth 启用健康检查
func WithHealth(enabled bool) Option {
	return func(o *Options) {
		o.Health.Enabled = enabled
	}
}

// WithHealthPort 设置健康检查端口
func WithHealthPort(port int) Option {
	return func(o *Options) {
		o.Health.Port = port
	}
}

// WithCustomLogger 设置自定义日志记录器
func WithCustomLogger(logger *log.Logger) Option {
	return func(o *Options) {
		o.CustomLogger = logger
	}
}
