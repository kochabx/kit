package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestRecoveryResumesAfterEmptyPage(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("recovery-cursor-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Execution: ExecutionConfig{RecoveryBatch: 1, RecoveryLimit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	q := s.queues[DefaultQueue]
	ctx := context.Background()
	if err := q.store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	for i := range 11 {
		if _, err := client.XAdd(ctx, &redis.XAddArgs{Stream: q.store.Keys.Ready(), Values: map[string]any{"job_id": fmt.Sprintf("job-%d", i)}}).Result(); err != nil {
			t.Fatal(err)
		}
	}
	deliveries, err := q.store.ReadDeliveries(ctx, "reader", time.Millisecond, 11)
	if err != nil || len(deliveries) != 11 {
		t.Fatalf("receive: %d %v", len(deliveries), err)
	}
	tail := deliveries[10]
	if err := client.Do(ctx, "XCLAIM", q.store.Keys.Ready(), q.store.Keys.Group(), "reader", 0, tail.MessageID, "IDLE", 3600000, "JUSTID").Err(); err != nil {
		t.Fatal(err)
	}
	recovered := make(chan store.Delivery, 1)
	cursor, err := q.recoverPending(ctx, recovered, "0-0")
	if err != nil || cursor == "0-0" || len(recovered) != 0 {
		t.Fatalf("first page: cursor=%s deliveries=%d err=%v", cursor, len(recovered), err)
	}
	cursor, err = q.recoverPending(ctx, recovered, cursor)
	if err != nil || cursor != "0-0" || len(recovered) != 1 {
		t.Fatalf("next page: cursor=%s deliveries=%d err=%v", cursor, len(recovered), err)
	}
	if got := <-recovered; got.JobID != tail.JobID || got.MessageID != tail.MessageID || !got.Recovered {
		t.Fatalf("recovered=%+v want=%+v", got, tail)
	}
}
