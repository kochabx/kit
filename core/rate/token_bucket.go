package rate

import (
	"context"
	_ "embed"
	"fmt"
	"math"

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
func NewTokenBucket(
	client redis.UniversalClient,
	rate float64,
	burst int64,
	opts ...Option,
) (*TokenBucket, error) {
	if err := validateClient(client); err != nil {
		return nil, err
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
		return nil, fmt.Errorf("%w: rate must be finite and positive", ErrInvalidConfig)
	}
	if burst <= 0 {
		return nil, fmt.Errorf("%w: burst must be positive", ErrInvalidConfig)
	}
	if burst > maxExactLuaInteger || rate > float64(maxExactLuaInteger)/1000 {
		return nil, fmt.Errorf("%w: numeric parameters are too large", ErrInvalidConfig)
	}
	refillMillis := float64(burst) / rate * 1000
	if math.IsInf(refillMillis, 0) || refillMillis > float64(maxDurationMillis-1000) {
		return nil, fmt.Errorf("%w: full refill exceeds supported duration", ErrInvalidConfig)
	}
	settings, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
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
	redisKey, err := validateRequest(ctx, l.keyPrefix, key, n, l.burst)
	if err != nil {
		return Result{}, err
	}
	return runScript(
		ctx,
		l.client,
		tokenBucketLuaScript,
		"token bucket",
		redisKey,
		l.burst,
		l.rate,
		l.burst,
		n,
	)
}
