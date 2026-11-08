package rate

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	//go:embed sliding_window.lua
	slidingWindowLua       string
	slidingWindowLuaScript = redis.NewScript(slidingWindowLua)
)

// SlidingWindow is an exact sliding-window limiter with weighted requests.
type SlidingWindow struct {
	client    redis.UniversalClient
	limit     int64
	window    int64 // Window length in seconds.
	keyPrefix string
}

var _ Limiter = (*SlidingWindow)(nil)

// NewSlidingWindow creates an exact sliding-window limiter.
func NewSlidingWindow(client redis.UniversalClient, limit, window int64, opts ...Option) (*SlidingWindow, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if limit <= 0 {
		return nil, errors.New("rate: limit must be positive")
	}
	if limit > maxExactLuaInteger {
		return nil, errors.New("rate: limit exceeds Lua integer precision")
	}
	if window <= 0 {
		return nil, errors.New("rate: window must be positive")
	}
	if window > maxExactLuaInteger/1000 {
		return nil, errors.New("rate: window is too large")
	}
	settings := applyOptions(opts)
	return &SlidingWindow{
		client:    client,
		limit:     limit,
		window:    window,
		keyPrefix: fmt.Sprintf("%s:sliding-window:%d:%d:", settings.keyPrefix, limit, window),
	}, nil
}

// Allow requests one quota unit.
func (l *SlidingWindow) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN requests n quota units, recording one weighted event regardless of n.
func (l *SlidingWindow) AllowN(ctx context.Context, key string, n int64) (Result, error) {
	if err := validateN(n, l.limit); err != nil {
		return Result{}, err
	}
	redisKey, err := buildRedisKey(l.keyPrefix, key)
	if err != nil {
		return Result{}, err
	}
	raw, err := slidingWindowLuaScript.Run(
		ctx, l.client, []string{redisKey},
		l.window, l.limit, n, uuid.NewString(),
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate: sliding window script: %w", err)
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
