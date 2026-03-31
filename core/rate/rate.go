package rate

import (
	"context"
	"time"
)

// Result 限流判定结果
type Result struct {
	// Allowed 是否放行
	Allowed bool
	// Remaining 当前窗口/桶内剩余配额
	Remaining int64
	// Limit 总配额上限
	Limit int64
	// RetryAfter 被拒绝时建议的重试等待时间；放行时为 0
	RetryAfter time.Duration
	// ResetAt 当前窗口/桶配额重置的时间点
	ResetAt time.Time
}

// Limiter 分布式限流器接口
type Limiter interface {
	// Allow 对 key 请求 n 个配额。
	// n <= 0 等价于 n = 1。
	Allow(ctx context.Context, key string, n int) (Result, error)
}
