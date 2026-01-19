package rate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/store/redis"
)

func TestSlidingWindowAllow(t *testing.T) {
	// 创建 Redis 客户端配置
	cfg := &redis.Config{
		Addrs:    []string{"localhost:6379"},
		Password: "12345678",
		DB:       0,
	}

	// 创建 Redis 客户端
	ctx := context.Background()
	client, err := redis.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// 创建滑动窗口限流器
	bucket := NewSlidingWindowLimiter(client.UniversalClient(), "test", 5, 10)
	var count int
	for i := 0; i < 1000; i++ {
		now := time.Now()
		if bucket.Allow() {
			count++
			fmt.Printf("Request allowed [M: %v] [S: %v] [count: %v]\n", now.Minute(), now.Second(), count)
		} else {
			fmt.Printf("Request denied [M: %v] [S: %v] [count: %v]\n", now.Minute(), now.Second(), count)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
