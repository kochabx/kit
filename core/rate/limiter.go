package rate

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const maxExactLuaInteger int64 = 1<<53 - 1
const maxDurationMillis int64 = int64((1<<63 - 1) / time.Millisecond)

// Limiter atomically consumes quota for a key.
type Limiter interface {
	AllowN(ctx context.Context, key string, n int64) (Result, error)
}

// Result describes a decision using the Redis server clock. ResetAt estimates
// when all quota will be restored. RetryAfter is zero for allowed requests.
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration
	ResetAt    time.Time
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func validateClient(client redis.UniversalClient) error {
	if isNil(client) {
		return ErrNilClient
	}
	return nil
}

func validateWindow(limit int64, window time.Duration) (int64, error) {
	if limit <= 0 || limit > maxExactLuaInteger {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidConfig, maxExactLuaInteger)
	}
	if window < time.Millisecond || window%time.Millisecond != 0 {
		return 0, fmt.Errorf("%w: window must be positive whole milliseconds", ErrInvalidConfig)
	}
	return window.Milliseconds(), nil
}

func validateRequest(ctx context.Context, prefix, key string, n, limit int64) (string, error) {
	if isNil(ctx) {
		return "", ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if n <= 0 {
		return "", ErrInvalidN
	}
	if n > limit {
		return "", ErrNTooLarge
	}
	return buildRedisKey(prefix, key)
}

func buildRedisKey(prefix, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", ErrInvalidKey
	}
	return prefix + key, nil
}
