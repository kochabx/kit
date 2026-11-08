package rate

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type replyClient struct {
	redis.UniversalClient
	reply any
	err   error
	calls int
}

func (c *replyClient) EvalSha(ctx context.Context, _ string, _ []string, _ ...any) *redis.Cmd {
	c.calls++
	command := redis.NewCmd(ctx)
	command.SetVal(c.reply)
	command.SetErr(c.err)
	return command
}

type constructor func(redis.UniversalClient, ...Option) (Limiter, error)

func constructors() []constructor {
	return []constructor{
		func(c redis.UniversalClient, o ...Option) (Limiter, error) {
			return NewTokenBucket(c, 10, 100, o...)
		},
		func(c redis.UniversalClient, o ...Option) (Limiter, error) {
			return NewFixedWindow(c, 100, time.Second, o...)
		},
		func(c redis.UniversalClient, o ...Option) (Limiter, error) {
			return NewSlidingWindow(c, 100, time.Second, o...)
		},
	}
}

func TestClientAndOptionsValidation(t *testing.T) {
	var typedNil *redis.Client
	for _, create := range constructors() {
		for _, client := range []redis.UniversalClient{nil, typedNil} {
			_, err := create(client)
			require.ErrorIs(t, err, ErrNilClient)
		}
		for _, option := range []Option{
			nil,
			WithKeyPrefix(""),
			WithKeyPrefix(" :\t "),
			WithKeyPrefix(":\u3000:"),
		} {
			_, err := create(&replyClient{}, option)
			require.ErrorIs(t, err, ErrInvalidOption)
		}
	}
}

func TestWindowValidation(t *testing.T) {
	for _, create := range []func(redis.UniversalClient, int64, time.Duration, ...Option) (Limiter, error){
		func(c redis.UniversalClient, l int64, w time.Duration, o ...Option) (Limiter, error) {
			return NewFixedWindow(c, l, w, o...)
		},
		func(c redis.UniversalClient, l int64, w time.Duration, o ...Option) (Limiter, error) {
			return NewSlidingWindow(c, l, w, o...)
		},
	} {
		for _, window := range []time.Duration{
			0,
			-time.Second,
			time.Nanosecond,
			time.Millisecond + time.Nanosecond,
		} {
			_, err := create(&replyClient{}, 1, window)
			require.ErrorIs(t, err, ErrInvalidConfig)
		}
		for _, limit := range []int64{0, -1, maxExactLuaInteger + 1} {
			_, err := create(&replyClient{}, limit, time.Second)
			require.ErrorIs(t, err, ErrInvalidConfig)
		}
		_, err := create(&replyClient{}, 1, time.Millisecond)
		require.NoError(t, err)
	}
}

func TestTokenBucketNumericBounds(t *testing.T) {
	for _, tc := range []struct {
		rate  float64
		burst int64
	}{
		{0, 1},
		{-1, 1},
		{math.NaN(), 1},
		{math.Inf(1), 1},
		{math.SmallestNonzeroFloat64, 1},
		{1, 0},
		{1, maxExactLuaInteger + 1},
		{float64(maxExactLuaInteger), 1},
		{1, maxExactLuaInteger},
	} {
		_, err := NewTokenBucket(&replyClient{}, tc.rate, tc.burst)
		require.ErrorIs(t, err, ErrInvalidConfig)
	}
	_, err := NewTokenBucket(&replyClient{}, 0.5, 10)
	require.NoError(t, err)
}

func TestRequestValidationBeforeRedis(t *testing.T) {
	for _, create := range constructors() {
		client := &replyClient{}
		limiter, err := create(client)
		require.NoError(t, err)
		for _, tc := range []struct {
			ctx context.Context
			key string
			n   int64
			err error
		}{
			{nil, "key", 1, ErrNilContext},
			{t.Context(), "key", 0, ErrInvalidN},
			{t.Context(), "key", 101, ErrNTooLarge},
			{t.Context(), " \t", 1, ErrInvalidKey},
		} {
			_, err := limiter.AllowN(tc.ctx, tc.key, tc.n)
			require.ErrorIs(t, err, tc.err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = limiter.AllowN(ctx, "key", 1)
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, client.calls)
	}
}

func TestKeysPreserveIdentity(t *testing.T) {
	first, err := buildRedisKey("prefix:", " user:42 ")
	require.NoError(t, err)
	second, err := buildRedisKey("prefix:", "user:42")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, "prefix: user:42 ", first)
	limiter, err := NewFixedWindow(&replyClient{}, 10, 250*time.Millisecond, WithKeyPrefix(" orders: "))
	require.NoError(t, err)
	require.Equal(t, "orders:fixed-window:10:250:", limiter.keyPrefix)
}

func TestRedisErrorChain(t *testing.T) {
	cause := errors.New("Redis unavailable")
	for _, create := range constructors() {
		limiter, err := create(&replyClient{err: cause})
		require.NoError(t, err)
		_, err = limiter.AllowN(t.Context(), "key", 1)
		require.ErrorIs(t, err, cause)
		require.NotErrorIs(t, err, ErrInvalidResult)
	}
}
