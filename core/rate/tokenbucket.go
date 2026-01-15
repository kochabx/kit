package rate

import (
	"context"
	_ "embed"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed tokenbucket.lua
	tokenBucketLua       string
	tokenBucketLuaScript = redis.NewScript(tokenBucketLua)
)

type TokenBucketLimiter struct {
	client    *redis.Client
	bucketKey string
	capacity  int
	rate      int
	script    *redis.Script
}

func NewTokenBucketLimiter(client *redis.Client, bucketKey string, capacity, rate int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		client:    client,
		bucketKey: bucketKey,
		capacity:  capacity,
		rate:      rate,
		script:    tokenBucketLuaScript,
	}
}

func (lim *TokenBucketLimiter) Allow() bool {
	return lim.AllowN(time.Now(), 1)
}

func (lim *TokenBucketLimiter) AllowN(t time.Time, n int) bool {
	return lim.AllowNCtx(context.Background(), t, n)
}

func (lim *TokenBucketLimiter) AllowNCtx(ctx context.Context, t time.Time, n int) bool {
	return lim.reserveN(ctx, t, n)
}

func (lim *TokenBucketLimiter) reserveN(ctx context.Context, t time.Time, n int) bool {
	now := t.Unix()
	result, err := lim.script.Run(ctx, lim.client, []string{lim.bucketKey}, lim.capacity, lim.rate, now, n).Result()
	if err != nil {
		return false
	}

	return result.(int64) == 1
}
