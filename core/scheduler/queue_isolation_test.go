package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueWorkerPoolsAreIsolated(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-isolation-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, "slow", "fast")
	// A task name can exist in both queues with independent handlers.
	slow := mustTask[int]("task", WithQueue("slow"))
	fast := mustTask[int]("task", WithQueue("fast"))
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	releaseOnce := sync.OnceFunc(func() { close(release) })
	var slowActive, slowMax, fastDone atomic.Int64
	if err := mustBind(s, slow).Handle(func(ctx context.Context, _ int) error {
		current := slowActive.Add(1)
		defer slowActive.Add(-1)
		slowMax.Store(max(slowMax.Load(), current))
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := mustBind(s, fast).Handle(func(context.Context, int) error { fastDone.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	t.Cleanup(func() { releaseOnce() })
	for range 5 {
		if _, err := mustBind(s, slow).Enqueue(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow queue never started")
	}
	for range 3 {
		if _, err := mustBind(s, fast).Enqueue(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, time.Second, func() bool {
		stats, err := mustQueue(s, "fast").Stats(ctx)
		return fastDone.Load() == 3 && err == nil && stats.Running == 0 && stats.Pending == 0
	})
	if slowActive.Load() != 1 || slowMax.Load() != 1 {
		t.Fatalf("slow queue concurrency active=%d max=%d", slowActive.Load(), slowMax.Load())
	}
	slowStats, err := mustQueue(s, "slow").Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fastStats, err := mustQueue(s, "fast").Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	total, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if slowStats.Running != 1 || fastStats.Running != 0 || total.Running != 1 {
		t.Fatalf("slow=%+v fast=%+v total=%+v", slowStats, fastStats, total)
	}
	if total.Ready != slowStats.Ready+fastStats.Ready || total.Pending != slowStats.Pending+fastStats.Pending {
		t.Fatal("aggregate stats differ from queue sums")
	}
	releaseOnce()
}
