// Package rate 提供基于 Redis 的分布式限流器。
package rate

import (
	"context"
	"errors"
	"strings"
	"time"
)

const maxExactLuaInteger int64 = 1<<53 - 1

var (
	ErrNilClient  = errors.New("rate: redis client is nil")
	ErrInvalidKey = errors.New("rate: key must not be empty")
	ErrInvalidN   = errors.New("rate: n must be positive")
	ErrNTooLarge  = errors.New("rate: n exceeds limiter capacity")
)

// Result 表示限流判定结果。ResetAt 表示限流器预计恢复全部配额的时间；
// 请求被允许时 RetryAfter 为零，否则表示再次申请相同数量配额前的最短等待时间。
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration
	ResetAt    time.Time
}

func validateN(n, limit int64) error {
	if n <= 0 {
		return ErrInvalidN
	}
	if n > limit {
		return ErrNTooLarge
	}
	return nil
}

// Limiter 对 key 对应的配额执行原子扣减。
type Limiter interface {
	AllowN(ctx context.Context, key string, n int64) (Result, error)
}

type options struct {
	keyPrefix string
}

// Option 用于配置限流器。
type Option func(*options)

// WithKeyPrefix 设置 Redis key 前缀，默认为 "rate"。
func WithKeyPrefix(prefix string) Option {
	prefix = strings.Trim(prefix, ": ")
	return func(opts *options) {
		if prefix != "" {
			opts.keyPrefix = prefix
		}
	}
}

func applyOptions(optionList []Option) options {
	opts := options{keyPrefix: "rate"}
	for _, option := range optionList {
		if option != nil {
			option(&opts)
		}
	}
	return opts
}

func buildRedisKey(prefix, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", ErrInvalidKey
	}
	return prefix + key, nil
}
