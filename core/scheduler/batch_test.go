package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestBatchChunksAndCoalescesDuplicateReads(t *testing.T) {
	client := testRedis(t)
	counter := &commandCounter{}
	client.AddHook(counter)
	namespace := fmt.Sprintf("batch-bounds-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bound := mustBind(s, mustTask[int]("batch"))
	values := make([]int, store.MaxBatchJobs+1)
	for i := range values {
		values[i] = i
	}
	counter.reset()
	results := bound.EnqueueBatch(context.Background(), values)
	for i, r := range results {
		if r.Err != nil || string(r.Job.Payload) != fmt.Sprint(i) {
			t.Fatalf("row %d: %+v", i, r)
		}
	}
	if counter.pipelines != 2 {
		t.Fatalf("chunk pipelines=%d", counter.pipelines)
	}
	counter.reset()
	duplicates := bound.EnqueueBatch(context.Background(), []int{1, 2, 3}, Unique("same", time.Hour))
	if duplicates[0].Err != nil {
		t.Fatal(duplicates[0].Err)
	}
	for _, r := range duplicates[1:] {
		if !errors.Is(r.Err, ErrDuplicate) || r.Job == nil || r.Job.ID != duplicates[0].Job.ID {
			t.Fatalf("duplicate: %+v", r)
		}
	}
	reads := 0
	for _, cmd := range counter.commands {
		if cmd == "hgetall" {
			reads++
		}
	}
	if reads != 1 || counter.pipelines != 2 {
		t.Fatalf("duplicate reads=%d pipelines=%d", reads, counter.pipelines)
	}
}

func TestStatsUsesOnePipelineForAllQueues(t *testing.T) {
	client := testRedis(t)
	counter := &commandCounter{}
	client.AddHook(counter)
	namespace := fmt.Sprintf("stats-pipeline-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, "a", "b")
	ctx := context.Background()
	for _, q := range s.queueOrder {
		if err := q.store.EnsureConsumerGroup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	future := mustBind(s, mustTask[int]("future", WithQueue("a")))
	if _, err := future.Enqueue(ctx, 1, At(time.Now().Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	// Force the delayed index into the past to exercise the server-clock lag calculation.
	if err := client.ZAdd(ctx, s.queues["a"].store.Keys.Scheduled(), redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: "delayed"}).Err(); err != nil {
		t.Fatal(err)
	}
	counter.reset()
	stats, err := s.Stats(ctx)
	if err != nil || stats.Ready != 1 || stats.Scheduled != 1 || stats.DispatchLag <= 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if counter.pipelines != 1 || len(counter.commands) != 2 {
		t.Fatalf("pipelines=%d commands=%v", counter.pipelines, counter.commands)
	}
}

func TestBatchChunksByPayloadBytes(t *testing.T) {
	client := testRedis(t)
	counter := &commandCounter{}
	client.AddHook(counter)
	namespace := fmt.Sprintf("batch-bytes-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, MaxPayloadBytes: 3 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bound := mustBind(s, mustTask[string]("large"))
	payload := strings.Repeat("x", store.MaxBatchPayloadBytes/2+1)
	counter.reset()
	results := bound.EnqueueBatch(context.Background(), []string{payload, payload})
	for _, result := range results {
		if result.Err != nil || len(result.Job.Payload) != len(payload)+2 {
			t.Fatalf("result: err=%v", result.Err)
		}
	}
	if counter.pipelines != 2 {
		t.Fatalf("payload budget pipelines=%d", counter.pipelines)
	}
}

func TestStatsPropagatesQueueStorageErrors(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("stats-errors-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, "healthy", "broken")
	ctx := context.Background()
	if err := client.Set(ctx, s.queues["broken"].store.Keys.Scheduled(), "wrong type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stats(ctx); err == nil {
		t.Fatal("aggregate hid queue storage error")
	}
	if _, err := mustQueue(s, "broken").Stats(ctx); err == nil {
		t.Fatal("queue hid storage error")
	}
}
