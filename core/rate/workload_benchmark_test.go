package rate

import (
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// BenchmarkSlidingWindowEvents measures exact-window costs at fixed occupancy.
func BenchmarkSlidingWindowEvents(b *testing.B) {
	for _, size := range []int{32, 1024, 8192} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			client, prefix := integrationClient(b)
			limiter, err := NewSlidingWindow(client, int64(size), time.Hour, WithKeyPrefix(prefix))
			require.NoError(b, err)
			key, err := buildRedisKey(limiter.keyPrefix, "benchmark")
			require.NoError(b, err)
			now, err := client.Time(b.Context()).Result()
			require.NoError(b, err)
			members := make([]redis.Z, size)
			for index := range members {
				members[index] = redis.Z{Score: float64(now.UnixMilli()), Member: fmt.Sprintf("1:%d", index)}
			}
			require.NoError(b, client.ZAdd(b.Context(), key, members...).Err())
			require.NoError(b, client.Expire(b.Context(), key, time.Hour).Err())
			b.ReportAllocs()
			for b.Loop() {
				result, err := limiter.Allow(b.Context(), "benchmark")
				if err != nil || result.Allowed {
					b.Fatalf("expected denial at full occupancy: %+v, %v", result, err)
				}
			}
		})
	}
}
