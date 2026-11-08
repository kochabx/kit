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

// TokenBucket is a distributed token-bucket limiter.
type TokenBucket struct {
	client    redis.UniversalClient
	rate      float64 // Tokens replenished per second.
	burst     int64   // Maximum burst capacity.
	keyPrefix string
}

var _ Limiter = (*TokenBucket)(nil)

// NewTokenBucket creates a limiter with the specified refill rate and burst capacity.
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

// Allow requests one quota unit.
func (l *TokenBucket) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN requests n quota units.
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
