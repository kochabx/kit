package rate

import (
	"context"
	_ "embed"
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
	client       redis.UniversalClient
	limit        int64
	windowMillis int64
	keyPrefix    string
}

var _ Limiter = (*SlidingWindow)(nil)

// NewSlidingWindow creates an exact sliding-window limiter.
// window must be a positive whole number of milliseconds.
func NewSlidingWindow(
	client redis.UniversalClient,
	limit int64,
	window time.Duration,
	opts ...Option,
) (*SlidingWindow, error) {
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
	return &SlidingWindow{
		client:       client,
		limit:        limit,
		windowMillis: windowMillis,
		keyPrefix:    fmt.Sprintf("%s:sliding-window:%d:%d:", settings.keyPrefix, limit, windowMillis),
	}, nil
}

// Allow requests one quota unit.
func (l *SlidingWindow) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN requests n quota units, recording one weighted event regardless of n.
func (l *SlidingWindow) AllowN(ctx context.Context, key string, n int64) (Result, error) {
	redisKey, err := validateRequest(ctx, l.keyPrefix, key, n, l.limit)
	if err != nil {
		return Result{}, err
	}
	return runScript(
		ctx,
		l.client,
		slidingWindowLuaScript,
		"sliding window",
		redisKey,
		l.limit,
		l.windowMillis,
		l.limit,
		n,
		uuid.NewString(),
	)
}
