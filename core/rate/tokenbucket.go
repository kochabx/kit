package rate

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed tokenbucket.lua
	tokenBucketLua       string
	tokenBucketLuaScript = redis.NewScript(tokenBucketLua)
)

// TokenBucketLimiter 基于 Redis + Lua 的分布式令牌桶限流器。
type TokenBucketLimiter struct {
	client   redis.UniversalClient
	capacity int // 桶容量
	rate     int // 每秒填充令牌数
}

// NewTokenBucketLimiter 创建令牌桶限流器。
//   - capacity: 桶的最大令牌数（突发容量）
//   - rate: 每秒填充的令牌数
func NewTokenBucketLimiter(client redis.UniversalClient, capacity, rate int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		client:   client,
		capacity: capacity,
		rate:     rate,
	}
}

// Allow 实现 Limiter 接口。
func (l *TokenBucketLimiter) Allow(ctx context.Context, key string, n int) (Result, error) {
	if n <= 0 {
		n = 1
	}

	nowMs := time.Now().UnixMilli()

	raw, err := tokenBucketLuaScript.Run(ctx, l.client, []string{key},
		l.capacity, l.rate, nowMs, n,
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate: token bucket script error: %w", err)
	}

	allowed := raw[0] == 1
	remaining := raw[1]

	res := Result{
		Allowed:   allowed,
		Remaining: remaining,
		Limit:     int64(l.capacity),
	}

	if !allowed {
		// 需要等待 deficit 个令牌填充的时间
		deficit := max(int64(n)-remaining, 0)
		res.RetryAfter = time.Duration(deficit) * time.Second / time.Duration(l.rate)
	}

	// 从空桶填满的时间
	res.ResetAt = time.Now().Add(time.Duration(int64(l.capacity)-remaining) * time.Second / time.Duration(l.rate))

	return res, nil
}
