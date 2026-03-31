package rate

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestFixedWindowAllow(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	// 5 秒窗口, 最多 10 个请求
	lim := NewFixedWindowLimiter(client.UniversalClient(), 5*time.Second, 10)
	key := fmt.Sprintf("test:fixedwindow:%d", time.Now().UnixNano())

	// 连续 10 个请求应该成功
	for i := 0; i < 10; i++ {
		res, err := lim.Allow(ctx, key, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
		t.Logf("request %d: allowed=%v remaining=%d", i, res.Allowed, res.Remaining)
	}

	// 第 11 个应该被拒绝
	res, err := lim.Allow(ctx, key, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("request should be denied after limit reached")
	}
}

func TestFixedWindowBatchN(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	lim := NewFixedWindowLimiter(client.UniversalClient(), 5*time.Second, 10)
	key := fmt.Sprintf("test:fixedwindow:batch:%d", time.Now().UnixNano())

	// 批量请求 7 个
	res, err := lim.Allow(ctx, key, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("batch request for 7 should be allowed")
	}
	if res.Remaining != 3 {
		t.Fatalf("expected 3 remaining, got %d", res.Remaining)
	}

	// 再请求 4 个，应被拒绝
	res, err = lim.Allow(ctx, key, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("request for 4 should be denied with only 3 remaining")
	}
}

func TestFixedWindowExpiry(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	// 2 秒窗口, 最多 2 个请求
	lim := NewFixedWindowLimiter(client.UniversalClient(), 2*time.Second, 2)
	key := fmt.Sprintf("test:fixedwindow:expiry:%d", time.Now().UnixNano())

	// 用尽配额
	lim.Allow(ctx, key, 2)

	res, _ := lim.Allow(ctx, key, 1)
	if res.Allowed {
		t.Fatal("should be denied after exhaustion")
	}

	// 等待窗口过期
	time.Sleep(2100 * time.Millisecond)

	res, err := lim.Allow(ctx, key, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("should be allowed after window expiry")
	}
}

func TestFixedWindowResult(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	lim := NewFixedWindowLimiter(client.UniversalClient(), 10*time.Second, 100)
	key := fmt.Sprintf("test:fixedwindow:result:%d", time.Now().UnixNano())

	res, err := lim.Allow(ctx, key, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Limit != 100 {
		t.Fatalf("expected limit=100, got %d", res.Limit)
	}
	if res.Remaining != 99 {
		t.Fatalf("expected remaining=99, got %d", res.Remaining)
	}
	if !res.Allowed {
		t.Fatal("should be allowed")
	}
	if res.ResetAt.IsZero() {
		t.Fatal("ResetAt should not be zero")
	}
}
