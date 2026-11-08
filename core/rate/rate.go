// Package rate provides Redis-backed distributed rate limiters.
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

// Result describes a rate-limit decision. ResetAt estimates when all quota will be restored.
// RetryAfter is zero for allowed requests; otherwise it estimates the minimum wait before requesting the same quota again.
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

// Limiter atomically consumes quota for a key.
type Limiter interface {
	AllowN(ctx context.Context, key string, n int64) (Result, error)
}

type options struct {
	keyPrefix string
}

// Option configures a limiter.
type Option func(*options)

// WithKeyPrefix sets the Redis key prefix, which defaults to "rate".
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
