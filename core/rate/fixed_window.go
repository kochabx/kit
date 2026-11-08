package rate

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed fixed_window.lua
	fixedWindowLua       string
	fixedWindowLuaScript = redis.NewScript(fixedWindowLua)
)

// FixedWindow is a distributed limiter aligned to fixed time boundaries.
type FixedWindow struct {
	client       redis.UniversalClient
	limit        int64
	windowMillis int64
	keyPrefix    string
}

var _ Limiter = (*FixedWindow)(nil)

// NewFixedWindow creates a limiter aligned to fixed time boundaries.
// window must be a positive whole number of milliseconds.
func NewFixedWindow(
	client redis.UniversalClient,
	limit int64,
	window time.Duration,
	opts ...Option,
) (*FixedWindow, error) {
	if err := validateClient(client); err != nil {
		return nil, err
	}
	windowMillis, err := validateWindow(limit, window)
	if err != nil {
		return nil, err
	}
	settings, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	return &FixedWindow{
		client:       client,
		limit:        limit,
		windowMillis: windowMillis,
		keyPrefix:    fmt.Sprintf("%s:fixed-window:%d:%d:", settings.keyPrefix, limit, windowMillis),
	}, nil
}

// Allow requests one quota unit.
func (l *FixedWindow) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN requests n quota units.
func (l *FixedWindow) AllowN(ctx context.Context, key string, n int64) (Result, error) {
	redisKey, err := validateRequest(ctx, l.keyPrefix, key, n, l.limit)
	if err != nil {
		return Result{}, err
	}
	return runScript(
		ctx,
		l.client,
		fixedWindowLuaScript,
		"fixed window",
		redisKey,
		l.limit,
		l.windowMillis,
		l.limit,
		n,
	)
}
