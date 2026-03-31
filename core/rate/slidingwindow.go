package rate

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	//go:embed slidingwindow.lua
	slidingWindowLua       string
	slidingWindowLuaScript = redis.NewScript(slidingWindowLua)
)

// SlidingWindowLimiter 基于 Redis ZSET 的分布式滑动窗口限流器。
type SlidingWindowLimiter struct {
	client redis.UniversalClient
	window time.Duration // 滑动窗口大小
	limit  int           // 窗口内最大请求数
}

// NewSlidingWindowLimiter 创建滑动窗口限流器。
//   - window: 滑动窗口时间范围
//   - limit: 窗口内允许的最大请求数
func NewSlidingWindowLimiter(client redis.UniversalClient, window time.Duration, limit int) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		client: client,
		window: window,
		limit:  limit,
	}
}

// Allow 实现 Limiter 接口。
func (l *SlidingWindowLimiter) Allow(ctx context.Context, key string, n int) (Result, error) {
	if n <= 0 {
		n = 1
	}

	nowMs := time.Now().UnixMilli()
	windowMs := l.window.Milliseconds()
	uid := uuid.New().String()

	raw, err := slidingWindowLuaScript.Run(ctx, l.client, []string{key},
		windowMs, l.limit, nowMs, n, uid,
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate: sliding window script error: %w", err)
	}

	allowed := raw[0] == 1
	count := raw[1]

	res := Result{
		Allowed:   allowed,
		Remaining: int64(l.limit) - count,
		Limit:     int64(l.limit),
		ResetAt:   time.UnixMilli(nowMs + windowMs),
	}

	if !allowed {
		// 最早的记录过期后才有配额释放，保守估计为整个窗口
		res.RetryAfter = l.window
	}

	return res, nil
}
