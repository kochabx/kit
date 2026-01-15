package rate

import (
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/store/redis"
)

func TestSlidingWindowAllow(t *testing.T) {
	r, err := redis.NewClient(&redis.SingleConfig{
		Password: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}

	bucket := NewSlidingWindowLimiter(r.Client, "test", 5, 10)
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
