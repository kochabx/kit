package rate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodeResult(t *testing.T) {
	result, err := decodeResult([]int64{1, 10, 9, 0, 1000}, 10)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Equal(t, int64(9), result.Remaining)
	require.Equal(t, time.UnixMilli(1000), result.ResetAt)
	result, err = decodeResult([]int64{0, 10, 0, 250, 2000}, 10)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Equal(t, 250*time.Millisecond, result.RetryAfter)
}

func TestMalformedResultsDoNotPanic(t *testing.T) {
	for _, raw := range [][]int64{
		nil,
		{1, 10, 9, 0},
		{1, 10, 9, 0, 1, 2},
		{2, 10, 9, 0, 1000},
		{1, 9, 9, 0, 1000},
		{1, 10, -1, 0, 1000},
		{1, 10, 11, 0, 1000},
		{0, 10, 0, -1, 1000},
		{0, 10, 0, maxDurationMillis + 1, 1000},
		{1, 10, 9, 0, 0},
		{1, 10, 9, 0, maxExactLuaInteger + 1},
		{1, 10, 9, 1, 1000},
		{0, 10, 0, 0, 1000},
	} {
		require.NotPanics(t, func() {
			_, err := decodeResult(raw, 10)
			require.ErrorIs(t, err, ErrInvalidResult)
		})
	}
}

func TestScriptResponseProtocol(t *testing.T) {
	for _, create := range constructors() {
		for _, reply := range []any{
			nil,
			"invalid",
			[]any{
				int64(1),
				"bad",
				int64(0),
				int64(0),
				int64(1000),
			},
			[]any{int64(1)},
		} {
			limiter, err := create(&replyClient{reply: reply})
			require.NoError(t, err)
			require.NotPanics(t, func() {
				_, err := limiter.AllowN(t.Context(), "key", 1)
				require.ErrorIs(t, err, ErrInvalidResult)
			})
		}
		limiter, err := create(&replyClient{reply: []any{
			int64(1),
			int64(100),
			int64(99),
			int64(0),
			int64(1000),
		}})
		require.NoError(t, err)
		result, err := limiter.AllowN(t.Context(), "key", 1)
		require.NoError(t, err)
		require.True(t, result.Allowed)
	}
}
