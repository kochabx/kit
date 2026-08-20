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

// SlidingWindow 是支持权重的精确滑动窗口限流器。
type SlidingWindow struct {
	client    redis.UniversalClient
	limit     int64
	window    int64 // 窗口秒数
	keyPrefix string
}

var _ Limiter = (*SlidingWindow)(nil)

// NewSlidingWindow 创建精确滑动窗口限流器。
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

// Allow 申请一个配额。
func (l *SlidingWindow) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN 申请 n 个配额。每次调用仅存储一个带权事件，Redis 写入复杂度不会随 n 增长。
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
