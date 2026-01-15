package rate

import (
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/store/redis"
)

func TestTokenBucketAllow(t *testing.T) {
	r, err := redis.NewClient(&redis.SingleConfig{
		Password: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	bucket := NewTokenBucketLimiter(r.Client, "test", 1, 1)
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
	r, err := redis.NewClient(&redis.SingleConfig{
		Password: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	bucket := NewTokenBucketLimiter(r.Client, "test", 2, 1)
	for i := 0; i < 1000; i++ {
		if bucket.AllowN(time.Now(), 2) {
			fmt.Printf("Request allowed [%v]\n", time.Now().Second())
		} else {
			fmt.Printf("Request denied [%v]\n", time.Now().Second())
		}
		time.Sleep(500 * time.Millisecond)
	}
}
