package rate

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed token_bucket.lua
	tokenBucketLua       string
	tokenBucketLuaScript = redis.NewScript(tokenBucketLua)
)

// TokenBucket 是分布式令牌桶限流器。
type TokenBucket struct {
	client    redis.UniversalClient
	rate      float64 // 每秒补充的令牌数
	burst     int64   // 最大突发容量
	keyPrefix string
}

var _ Limiter = (*TokenBucket)(nil)

// NewTokenBucket 创建指定补充速率和最大突发容量的令牌桶限流器。
func NewTokenBucket(client redis.UniversalClient, rate float64, burst int64, opts ...Option) (*TokenBucket, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
		return nil, errors.New("rate: rate must be finite and positive")
	}
	if burst <= 0 {
		return nil, errors.New("rate: burst must be positive")
	}
	if burst > maxExactLuaInteger || rate > float64(maxExactLuaInteger)/1000 {
		return nil, errors.New("rate: numeric parameters are too large")
	}
	settings := applyOptions(opts)
	return &TokenBucket{
		client:    client,
		rate:      rate,
		burst:     burst,
		keyPrefix: fmt.Sprintf("%s:token-bucket:%g:%d:", settings.keyPrefix, rate, burst),
	}, nil
}

// Allow 申请一个配额。
func (l *TokenBucket) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN 申请 n 个配额。
func (l *TokenBucket) AllowN(ctx context.Context, key string, n int64) (Result, error) {
	if err := validateN(n, l.burst); err != nil {
		return Result{}, err
	}
	redisKey, err := buildRedisKey(l.keyPrefix, key)
	if err != nil {
		return Result{}, err
	}
	raw, err := tokenBucketLuaScript.Run(
		ctx, l.client, []string{redisKey},
		l.rate, l.burst, n,
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate: token bucket script: %w", err)
	}
	result := Result{
		Allowed:   raw[0] == 1,
		Limit:     raw[1],
		Remaining: raw[2],
		ResetAt:   time.UnixMilli(raw[4]),
	}
	if !result.Allowed {
		result.RetryAfter = time.Duration(raw[3]) * time.Millisecond
	}
	return result, nil
}
