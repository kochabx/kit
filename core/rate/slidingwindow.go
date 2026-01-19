package rate

import (
	"context"
	_ "embed"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed slidingwindow.lua
	slidingWindowLua       string
	slidingWindowLuaScript = redis.NewScript(slidingWindowLua)
)

type SlidingWindowLimiter struct {
	client    redis.UniversalClient
	bucketKey string
	window    int
	limit     int
	script    *redis.Script
}

func NewSlidingWindowLimiter(client redis.UniversalClient, bucketKey string, window, limit int) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		client:    client,
		bucketKey: bucketKey,
		window:    window,
		limit:     limit,
		script:    slidingWindowLuaScript,
	}
}

func (limiter *SlidingWindowLimiter) Allow() bool {
	return limiter.AllowN(time.Now(), 1)
}

func (limiter *SlidingWindowLimiter) AllowN(t time.Time, n int) bool {
	return limiter.AllowNCtx(context.Background(), t, n)
}

func (limiter *SlidingWindowLimiter) AllowNCtx(ctx context.Context, t time.Time, n int) bool {
	return limiter.reserveN(ctx, t, n)
}

func (limiter *SlidingWindowLimiter) reserveN(ctx context.Context, t time.Time, n int) bool {
	now := t.Unix()
	result, err := limiter.script.Run(ctx, limiter.client, []string{limiter.bucketKey}, limiter.window, limiter.limit, now, n).Result()
	if err != nil {
		return false
	}

	return result.(int64) == 1
}
