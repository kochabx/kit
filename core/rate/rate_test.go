package rate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func testClient() redis.UniversalClient {
	return redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		MaxRetries:  -1,
		DialTimeout: 10 * time.Millisecond,
	})
}

func integrationClient(t *testing.T) redis.UniversalClient {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "12345678",
	})
	require.NoError(t, client.Ping(t.Context()).Err())
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func TestConstructorsValidateArguments(t *testing.T) {
	client := testClient()
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	_, err := NewTokenBucket(nil, 1, 1)
	require.ErrorIs(t, err, ErrNilClient)
	_, err = NewTokenBucket(client, 0, 1)
	require.Error(t, err)
	_, err = NewTokenBucket(client, 1, 0)
	require.Error(t, err)

	_, err = NewFixedWindow(client, 0, 1)
	require.Error(t, err)
	_, err = NewFixedWindow(client, 1, 0)
	require.Error(t, err)
	_, err = NewSlidingWindow(client, 0, 1)
	require.Error(t, err)
	_, err = NewSlidingWindow(client, 1, 0)
	require.Error(t, err)

}

func TestBuildRedisKey(t *testing.T) {
	first, err := buildRedisKey("rate:fixed-window:", " user:42 ")
	require.NoError(t, err)
	second, err := buildRedisKey("rate:fixed-window:", "user:42")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, "rate:fixed-window: user:42 ", first)
	require.Equal(t, "rate:fixed-window:user:42", second)
	_, err = buildRedisKey("rate:fixed-window:", "  ")
	require.ErrorIs(t, err, ErrInvalidKey)
}

func TestWithKeyPrefix(t *testing.T) {
	client := testClient()
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	limiter, err := NewTokenBucket(client, 10, 100, WithKeyPrefix(" orders: "))
	require.NoError(t, err)
	require.Equal(t, "orders:token-bucket:10:100:", limiter.keyPrefix)
}

func TestAllowValidatesInputBeforeRedis(t *testing.T) {
	client := testClient()
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	limiter, err := NewFixedWindow(client, 10, 1)
	require.NoError(t, err)

	_, err = limiter.AllowN(context.Background(), "key", 0)
	require.ErrorIs(t, err, ErrInvalidN)
	_, err = limiter.AllowN(context.Background(), "key", 11)
	require.ErrorIs(t, err, ErrNTooLarge)
	_, err = limiter.Allow(context.Background(), "")
	require.ErrorIs(t, err, ErrInvalidKey)
}

func TestRateLimitersIntegration(t *testing.T) {
	client := integrationClient(t)
	tests := []struct {
		name string
		new  func() (Limiter, error)
	}{
		{"token bucket", func() (Limiter, error) {
			return NewTokenBucket(client, 10, 10)
		}},
		{"fixed window", func() (Limiter, error) { return NewFixedWindow(client, 10, 1) }},
		{"sliding window", func() (Limiter, error) { return NewSlidingWindow(client, 10, 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter, err := tt.new()
			require.NoError(t, err)
			key := t.Name() + time.Now().String()

			result, err := limiter.AllowN(t.Context(), key, 7)
			require.NoError(t, err)
			require.True(t, result.Allowed)
			require.Equal(t, int64(3), result.Remaining)
			require.Zero(t, result.RetryAfter)
			require.True(t, result.ResetAt.After(time.Now().Add(-time.Second)))

			result, err = limiter.AllowN(t.Context(), key, 4)
			require.NoError(t, err)
			require.False(t, result.Allowed)
			require.Equal(t, int64(3), result.Remaining)
			require.Positive(t, result.RetryAfter)
		})
	}
}

func TestTokenBucketAllow(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewTokenBucket(client, 2, 5, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	for i := range 5 {
		result, allowErr := limiter.Allow(t.Context(), key)
		require.NoError(t, allowErr)
		require.Truef(t, result.Allowed, "第 %d 次请求应该被允许", i+1)
		require.Equal(t, int64(4-i), result.Remaining)
		t.Logf("第 %d 次请求：allowed=%v remaining=%d limit=%d", i+1, result.Allowed, result.Remaining, result.Limit)
	}

	result, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Zero(t, result.Remaining)
	require.Positive(t, result.RetryAfter)
	t.Logf("令牌耗尽：allowed=%v remaining=%d retryAfter=%v", result.Allowed, result.Remaining, result.RetryAfter)
}

func TestFixedWindowAllow(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewFixedWindow(client, 3, 10, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	for i := range 3 {
		result, allowErr := limiter.Allow(t.Context(), key)
		require.NoError(t, allowErr)
		require.Truef(t, result.Allowed, "第 %d 次请求应该被允许", i+1)
		require.Equal(t, int64(2-i), result.Remaining)
		t.Logf("第 %d 次请求：allowed=%v remaining=%d resetAt=%s", i+1, result.Allowed, result.Remaining, result.ResetAt)
	}

	result, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Zero(t, result.Remaining)
	require.Positive(t, result.RetryAfter)
	t.Logf("窗口配额耗尽：allowed=%v retryAfter=%v resetAt=%s", result.Allowed, result.RetryAfter, result.ResetAt)
}

func TestSlidingWindowAllow(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewSlidingWindow(client, 3, 10, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	for i := range 3 {
		result, allowErr := limiter.Allow(t.Context(), key)
		require.NoError(t, allowErr)
		require.Truef(t, result.Allowed, "第 %d 次请求应该被允许", i+1)
		require.Equal(t, int64(2-i), result.Remaining)
		t.Logf("第 %d 次请求：allowed=%v remaining=%d resetAt=%s", i+1, result.Allowed, result.Remaining, result.ResetAt)
	}

	result, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Zero(t, result.Remaining)
	require.Positive(t, result.RetryAfter)
	t.Logf("滑动窗口配额耗尽：allowed=%v retryAfter=%v resetAt=%s", result.Allowed, result.RetryAfter, result.ResetAt)
}

func TestTokenBucketIntegration(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewTokenBucket(client, 2, 5, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	result, err := limiter.AllowN(t.Context(), key, 5)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Zero(t, result.Remaining)
	require.Zero(t, result.RetryAfter)
	require.True(t, result.ResetAt.After(time.Now()))

	result, err = limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Zero(t, result.Remaining)
	require.Positive(t, result.RetryAfter)
	require.LessOrEqual(t, result.RetryAfter, 500*time.Millisecond)

	time.Sleep(result.RetryAfter + 30*time.Millisecond)
	result, err = limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.True(t, result.Allowed)
}

func TestTokenBucketFractionalRateIntegration(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewTokenBucket(client, 0.5, 30, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	result, err := limiter.AllowN(t.Context(), key, 30)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Zero(t, result.Remaining)

	result, err = limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.InDelta(t, 2*time.Second, result.RetryAfter, float64(100*time.Millisecond))
}

func TestFixedWindowIntegration(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewFixedWindow(client, 5, 1, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	result, err := limiter.AllowN(t.Context(), key, 3)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Equal(t, int64(2), result.Remaining)

	result, err = limiter.AllowN(t.Context(), key, 3)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Equal(t, int64(2), result.Remaining)
	require.Positive(t, result.RetryAfter)
	require.LessOrEqual(t, result.RetryAfter, time.Second)

	time.Sleep(result.RetryAfter + 30*time.Millisecond)
	result, err = limiter.AllowN(t.Context(), key, 5)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Zero(t, result.Remaining)
}

func TestSlidingWindowIntegration(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewSlidingWindow(client, 5, 1, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	result, err := limiter.AllowN(t.Context(), key, 2)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Equal(t, int64(3), result.Remaining)

	result, err = limiter.AllowN(t.Context(), key, 3)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Zero(t, result.Remaining)

	result, err = limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Positive(t, result.RetryAfter)
	require.LessOrEqual(t, result.RetryAfter, time.Second)

	time.Sleep(result.RetryAfter + 30*time.Millisecond)
	result, err = limiter.AllowN(t.Context(), key, 5)
	require.NoError(t, err)
	require.True(t, result.Allowed)
}

func TestFixedWindowConcurrentIntegration(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewFixedWindow(client, 50, 10, WithKeyPrefix("rate-test"))
	require.NoError(t, err)
	key := uniqueKey(t)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	errorsCh := make(chan error, 100)
	for range 100 {
		wg.Go(func() {
			result, allowErr := limiter.Allow(t.Context(), key)
			if allowErr != nil {
				errorsCh <- allowErr
				return
			}
			if result.Allowed {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	close(errorsCh)
	for allowErr := range errorsCh {
		require.NoError(t, allowErr)
	}
	require.Equal(t, int64(50), allowed.Load())
}

func TestKeyIsolationIntegration(t *testing.T) {
	client := integrationClient(t)
	first, err := NewFixedWindow(client, 1, 10, WithKeyPrefix("rate-test-a"))
	require.NoError(t, err)
	second, err := NewFixedWindow(client, 1, 10, WithKeyPrefix("rate-test-b"))
	require.NoError(t, err)
	key := uniqueKey(t)

	firstResult, err := first.Allow(t.Context(), key)
	require.NoError(t, err)
	secondResult, err := second.Allow(t.Context(), key)
	require.NoError(t, err)
	require.True(t, firstResult.Allowed)
	require.True(t, secondResult.Allowed)
}

func TestCanceledContextIntegration(t *testing.T) {
	client := integrationClient(t)
	limiter, err := NewTokenBucket(client, 1, 1)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = limiter.Allow(ctx, uniqueKey(t))
	require.ErrorIs(t, err, context.Canceled)
}

func uniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s:%d", t.Name(), time.Now().UnixNano())
}

func TestRedisErrorIsWrapped(t *testing.T) {
	client := testClient()
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	limiter, err := NewFixedWindow(client, 1, 1)
	require.NoError(t, err)
	_, err = limiter.Allow(t.Context(), "key")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrInvalidKey))
}
