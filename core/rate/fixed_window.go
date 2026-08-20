package rate

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed fixed_window.lua
	fixedWindowLua       string
	fixedWindowLuaScript = redis.NewScript(fixedWindowLua)
)

// FixedWindow 是按自然时间边界对齐的分布式固定窗口限流器。
type FixedWindow struct {
	client    redis.UniversalClient
	limit     int64
	window    int64 // 窗口秒数
	keyPrefix string
}

var _ Limiter = (*FixedWindow)(nil)

// NewFixedWindow 创建按自然时间边界对齐的固定窗口限流器。
func NewFixedWindow(client redis.UniversalClient, limit, window int64, opts ...Option) (*FixedWindow, error) {
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
	return &FixedWindow{
		client:    client,
		limit:     limit,
		window:    window,
		keyPrefix: fmt.Sprintf("%s:fixed-window:%d:%d:", settings.keyPrefix, limit, window),
	}, nil
}

// Allow 申请一个配额。
func (l *FixedWindow) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN 申请 n 个配额。
func (l *FixedWindow) AllowN(ctx context.Context, key string, n int64) (Result, error) {
	if err := validateN(n, l.limit); err != nil {
		return Result{}, err
	}
	redisKey, err := buildRedisKey(l.keyPrefix, key)
	if err != nil {
		return Result{}, err
	}
	raw, err := fixedWindowLuaScript.Run(
		ctx, l.client, []string{redisKey},
		l.window, l.limit, n,
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("rate: fixed window script: %w", err)
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
