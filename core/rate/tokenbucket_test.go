package rate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/store/redis"
)

func newTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	cfg := &redis.Config{
		Addrs:    []string{"localhost:6379"},
		Password: "12345678",
		DB:       0,
	}
	client, err := redis.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestTokenBucketAllow(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	lim := NewTokenBucketLimiter(client.UniversalClient(), 5, 2) // 容量5, 每秒填充2
	key := fmt.Sprintf("test:tokenbucket:%d", time.Now().UnixNano())

	// 连续请求应该成功（桶初始满）
	for i := 0; i < 5; i++ {
		res, err := lim.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
		t.Logf("request %d: allowed=%v remaining=%d limit=%d", i, res.Allowed, res.Remaining, res.Limit)
	}

	// 桶耗尽，应被拒绝
	res, err := lim.Allow(ctx, key, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("request should be denied after bucket exhausted")
	}
	if res.RetryAfter <= 0 {
		t.Fatal("RetryAfter should be positive when denied")
	}
	t.Logf("denied: remaining=%d retryAfter=%v", res.Remaining, res.RetryAfter)
}

func TestTokenBucketAllowN(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	lim := NewTokenBucketLimiter(client.UniversalClient(), 10, 5) // 容量10, 每秒填充5
	key := fmt.Sprintf("test:tokenbucket:n:%d", time.Now().UnixNano())

	// 批量请求 5 个令牌
	res, err := lim.Allow(ctx, key, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("bulk request should be allowed")
	}
	if res.Remaining != 5 {
		t.Fatalf("expected 5 remaining, got %d", res.Remaining)
	}

	// 再请求 6 个，应被拒绝（仅剩 5）
	res, err = lim.Allow(ctx, key, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("request for 6 tokens should be denied with only 5 remaining")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	lim := NewTokenBucketLimiter(client.UniversalClient(), 2, 2) // 容量2, 每秒填充2
	key := fmt.Sprintf("test:tokenbucket:refill:%d", time.Now().UnixNano())

	// 耗尽
	lim.Allow(ctx, key, 2)

	res, _ := lim.Allow(ctx, key, 1)
	if res.Allowed {
		t.Fatal("should be denied right after exhaustion")
	}

	// 等待 1.1 秒让令牌填充
	time.Sleep(1100 * time.Millisecond)

	res, err := lim.Allow(ctx, key, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("should be allowed after refill")
	}
	t.Logf("after refill: remaining=%d", res.Remaining)
}

func TestTokenBucketResult(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	lim := NewTokenBucketLimiter(client.UniversalClient(), 10, 5)
	key := fmt.Sprintf("test:tokenbucket:result:%d", time.Now().UnixNano())

	res, err := lim.Allow(ctx, key, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Limit != 10 {
		t.Fatalf("expected limit=10, got %d", res.Limit)
	}
	if res.Remaining != 7 {
		t.Fatalf("expected remaining=7, got %d", res.Remaining)
	}
	if !res.Allowed {
		t.Fatal("should be allowed")
	}
	if res.RetryAfter != 0 {
		t.Fatalf("RetryAfter should be 0 when allowed, got %v", res.RetryAfter)
	}
	if res.ResetAt.IsZero() {
		t.Fatal("ResetAt should not be zero")
	}
}
