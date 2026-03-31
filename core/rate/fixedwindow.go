package rate

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed fixedwindow.lua
	fixedWindowLua       string
	fixedWindowLuaScript = redis.NewScript(fixedWindowLua)
)

// FixedWindowLimiter 基于 Redis INCR + EXPIRE 的分布式固定窗口限流器。
type FixedWindowLimiter struct {
	client redis.UniversalClient
	window time.Duration // 固定窗口大小
	limit  int           // 窗口内最大请求数
}

// NewFixedWindowLimiter 创建固定窗口限流器。
//   - window: 固定窗口时间范围
//   - limit: 窗口内允许的最大请求数
func NewFixedWindowLimiter(client redis.UniversalClient, window time.Duration, limit int) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		client: client,
		window: window,
		limit:  limit,
	}
}

// Allow 实现 Limiter 接口。
func (l *FixedWindowLimiter) Allow(ctx context.Context, key string, n int) (Result, error) {
	if n <= 0 {
		n = 1
	}

	windowSec := int(l.window.Seconds())
	if windowSec < 1 {
		windowSec = 1
	}

	raw, err := fixedWindowLuaScript.Run(ctx, l.client, []string{key},
		windowSec, l.limit, n,
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate: fixed window script error: %w", err)
	}

	allowed := raw[0] == 1
	count := raw[1]

	res := Result{
		Allowed:   allowed,
		Remaining: int64(l.limit) - count,
		Limit:     int64(l.limit),
		ResetAt:   time.Now().Add(l.window),
	}

	if !allowed {
		res.RetryAfter = l.window
	}

	return res, nil
}
