package rate

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func integrationClient(t testing.TB) (redis.UniversalClient, string) {
	t.Helper()
	address := os.Getenv("RATE_REDIS_ADDR")
	if testing.Short() || address == "" {
		t.Skip("set RATE_REDIS_ADDR to run Redis integration tests")
	}
	client := redis.NewClient(&redis.Options{
		Addr:                  address,
		Password:              os.Getenv("RATE_REDIS_PASSWORD"),
		ContextTimeoutEnabled: true,
		MaxRetries:            -1,
		DialTimeout:           2 * time.Second,
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
	})
	prefix := "kit-rate-test:" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defer client.Close()
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, prefix+":*", 100).Result()
			if err != nil {
				t.Errorf("cleanup scan: %v", err)
				return
			}
			if len(keys) > 0 {
				if err := client.Del(ctx, keys...).Err(); err != nil {
					t.Errorf("cleanup delete: %v", err)
					return
				}
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	require.NoError(t, client.Ping(ctx).Err())
	return client, prefix
}

func TestLimitersIntegration(t *testing.T) {
	for index, create := range constructors() {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			client, prefix := integrationClient(t)
			limiter, err := create(client, WithKeyPrefix(prefix))
			require.NoError(t, err)
			result, err := limiter.AllowN(t.Context(), "quota", 70)
			require.NoError(t, err)
			require.True(t, result.Allowed)
			require.Equal(t, int64(30), result.Remaining)
			require.Zero(t, result.RetryAfter)
			result, err = limiter.AllowN(t.Context(), "quota", 40)
			require.NoError(t, err)
			require.False(t, result.Allowed)
			require.Positive(t, result.RetryAfter)
			require.InDelta(t, 30, result.Remaining, 1)
			require.True(t, result.ResetAt.After(time.Now().Add(-time.Second)))
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = limiter.AllowN(ctx, "quota", 1)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestMillisecondWindowsIntegration(t *testing.T) {
	for _, sliding := range []bool{false, true} {
		t.Run(fmt.Sprint(sliding), func(t *testing.T) {
			client, prefix := integrationClient(t)
			var limiter Limiter
			var err error
			if sliding {
				limiter, err = NewSlidingWindow(client, 1, 150*time.Millisecond, WithKeyPrefix(prefix))
			} else {
				limiter, err = NewFixedWindow(client, 1, 150*time.Millisecond, WithKeyPrefix(prefix))
			}
			require.NoError(t, err)
			first, err := limiter.AllowN(t.Context(), "window", 1)
			require.NoError(t, err)
			require.True(t, first.Allowed)
			// Exhaust the same fixed window even if the wall clock crosses its boundary.
			var denied Result
			require.Eventually(t, func() bool {
				var err error
				denied, err = limiter.AllowN(t.Context(), "window", 1)
				require.NoError(t, err)
				return !denied.Allowed
			}, time.Second, time.Millisecond)
			require.Positive(t, denied.RetryAfter)
			require.LessOrEqual(t, denied.RetryAfter, 150*time.Millisecond)
			time.Sleep(denied.RetryAfter + 20*time.Millisecond)
			next, err := limiter.AllowN(t.Context(), "window", 1)
			require.NoError(t, err)
			require.True(t, next.Allowed)
		})
	}
}

func TestTokenRefillIntegration(t *testing.T) {
	client, prefix := integrationClient(t)
	limiter, err := NewTokenBucket(client, 0.5, 5, WithKeyPrefix(prefix))
	require.NoError(t, err)
	first, err := limiter.AllowN(t.Context(), "bucket", 5)
	require.NoError(t, err)
	require.True(t, first.Allowed)
	next, err := limiter.Allow(t.Context(), "bucket")
	require.NoError(t, err)
	require.False(t, next.Allowed)
	require.InDelta(t, float64(2*time.Second), float64(next.RetryAfter), float64(200*time.Millisecond))
}

func TestConcurrentQuotaIntegration(t *testing.T) {
	for _, sliding := range []bool{false, true} {
		t.Run(fmt.Sprint(sliding), func(t *testing.T) {
			client, prefix := integrationClient(t)
			var limiter Limiter
			var err error
			if sliding {
				limiter, err = NewSlidingWindow(client, 50, time.Minute, WithKeyPrefix(prefix))
			} else {
				limiter, err = NewFixedWindow(client, 50, time.Minute, WithKeyPrefix(prefix))
			}
			require.NoError(t, err)
			var allowed atomic.Int64
			var group sync.WaitGroup
			failures := make(chan error, 100)
			for range 100 {
				group.Go(func() {
					result, err := limiter.AllowN(t.Context(), "shared", 1)
					if err != nil {
						failures <- err
						return
					}
					if result.Allowed {
						allowed.Add(1)
					}
				})
			}
			group.Wait()
			close(failures)
			for err := range failures {
				require.NoError(t, err)
			}
			require.Equal(t, int64(50), allowed.Load())
		})
	}
}

func TestKeyIsolationIntegration(t *testing.T) {
	client, prefix := integrationClient(t)
	first, err := NewFixedWindow(client, 1, time.Minute, WithKeyPrefix(prefix+":a"))
	require.NoError(t, err)
	second, err := NewFixedWindow(client, 1, time.Minute, WithKeyPrefix(prefix+":b"))
	require.NoError(t, err)
	for _, limiter := range []*FixedWindow{first, second} {
		result, err := limiter.Allow(t.Context(), "same")
		require.NoError(t, err)
		require.True(t, result.Allowed)
	}
}

func TestAlgorithmAndParameterIsolationIntegration(t *testing.T) {
	client, prefix := integrationClient(t)
	fixed, err := NewFixedWindow(client, 1, time.Minute, WithKeyPrefix(prefix))
	require.NoError(t, err)
	changedLimit, err := NewFixedWindow(client, 2, time.Minute, WithKeyPrefix(prefix))
	require.NoError(t, err)
	changedWindow, err := NewFixedWindow(client, 1, 2*time.Minute, WithKeyPrefix(prefix))
	require.NoError(t, err)
	sliding, err := NewSlidingWindow(client, 1, time.Minute, WithKeyPrefix(prefix))
	require.NoError(t, err)
	bucket, err := NewTokenBucket(client, 0.01, 1, WithKeyPrefix(prefix))
	require.NoError(t, err)
	for _, limiter := range []Limiter{fixed, changedLimit, changedWindow, sliding, bucket} {
		result, err := limiter.AllowN(t.Context(), "same", 1)
		require.NoError(t, err)
		require.True(t, result.Allowed)
		require.Equal(t, result.Limit-1, result.Remaining)
	}
	result, err := fixed.Allow(t.Context(), "same")
	require.NoError(t, err)
	require.False(t, result.Allowed)
}

func TestLargeSlidingWeightIntegration(t *testing.T) {
	client, prefix := integrationClient(t)
	limiter, err := NewSlidingWindow(client, maxExactLuaInteger, time.Minute, WithKeyPrefix(prefix))
	require.NoError(t, err)
	result, err := limiter.AllowN(t.Context(), "large", maxExactLuaInteger)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Zero(t, result.Remaining)
	result, err = limiter.Allow(t.Context(), "large")
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Zero(t, result.Remaining)
}
