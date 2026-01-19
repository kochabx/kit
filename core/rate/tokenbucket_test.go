package rate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/store/redis"
)

func TestTokenBucketAllow(t *testing.T) {
	cfg := &redis.Config{
		Addrs:    []string{"localhost:6379"},
		Password: "12345678",
		DB:       0,
	}

	ctx := context.Background()
	client, err := redis.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	bucket := NewTokenBucketLimiter(client.UniversalClient(), "test", 1, 1)
	for i := 0; i < 1000; i++ {
		if bucket.Allow() {
			fmt.Printf("Request allowed [%v]\n", time.Now().Second())
		} else {
			fmt.Printf("Request denied [%v]\n", time.Now().Second())
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	cfg := &redis.Config{
		Addrs:    []string{"localhost:6379"},
		Password: "12345678",
		DB:       0,
	}

	ctx := context.Background()
	client, err := redis.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	bucket := NewTokenBucketLimiter(client.UniversalClient(), "test", 2, 1)
	for i := 0; i < 1000; i++ {
		if bucket.AllowN(time.Now(), 2) {
			fmt.Printf("Request allowed [%v]\n", time.Now().Second())
		} else {
			fmt.Printf("Request denied [%v]\n", time.Now().Second())
		}
		time.Sleep(500 * time.Millisecond)
	}
}
