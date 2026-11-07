package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestDeadCursorContinuesThroughEmptyFilteredPages(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scan-pages-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	keys := s.queues[DefaultQueue].store.Keys
	pipe := client.Pipeline()
	for i := range 600 {
		id := fmt.Sprintf("job-%04d", i)
		name := "other"
		if i%7 == 0 {
			name = "selected"
		}
		pipe.HSet(ctx, keys.Job(id), "queue", DefaultQueue, "task", name, "state", string(JobDead))
		pipe.ZAdd(ctx, keys.Dead(), redis.Z{Score: float64(i), Member: id})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	queue := mustQueue(s, DefaultQueue)
	first, err := queue.DeadJobs(ctx, DeadQuery{Count: 1, TaskName: "missing"})
	if err != nil || len(first.Items) != 0 || first.NextCursor == 0 {
		t.Fatalf("empty page: %+v %v", first, err)
	}
	seen := map[string]bool{}
	cursor := uint64(0)
	pages := 0
	for {
		page, err := queue.DeadJobs(ctx, DeadQuery{Cursor: cursor, Count: 10, TaskName: "selected"})
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range page.Items {
			if job.TaskName != "selected" {
				t.Fatal("unfiltered job")
			}
			seen[job.ID] = true
		}
		pages++
		cursor = page.NextCursor
		if cursor == 0 {
			break
		}
		if pages > 1000 {
			t.Fatal("scan did not finish")
		}
	}
	if len(seen) != 86 || pages < 2 {
		t.Fatalf("items=%d pages=%d", len(seen), pages)
	}
}

func TestListScanArguments(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scan-validation-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	q := mustQueue(s, DefaultQueue)
	ctx := context.Background()
	for _, count := range []int64{-1, 0, 1001} {
		if _, err := q.DeadJobs(ctx, DeadQuery{Count: count}); err == nil {
			t.Fatal("invalid dead scan accepted")
		}
		if _, err := q.RunningJobs(ctx, Scan{Count: count}); err == nil {
			t.Fatal("invalid running scan accepted")
		}
		if _, err := q.Schedules(ctx, ScheduleQuery{Count: count}); err == nil {
			t.Fatal("invalid schedule scan accepted")
		}
	}
}
