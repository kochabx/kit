package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestJanitorCleansIndexesInBoundedBatches(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Maintenance: MaintenanceConfig{Batch: 2, DrainLimit: 4}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now, err := client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Dead(),
		redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "expired-1"},
		redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "expired-2"},
		redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: "retained"},
	).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Scheduled(), redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: "orphan-job"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Schedules(), redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: "orphan-cron"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.ScheduleCatalog(), redis.Z{Score: float64(now.UnixMilli()), Member: "orphan-cron"}).Err(); err != nil {
		t.Fatal(err)
	}

	var scheduledCursor, cronCursor uint64
	for range 10 {
		scheduledCursor, cronCursor, err = s.queues[DefaultQueue].maintainQueue(ctx, scheduledCursor, cronCursor)
		if err != nil {
			t.Fatal(err)
		}
		if scheduledCursor == 0 && cronCursor == 0 {
			break
		}
	}
	if got := client.ZCard(ctx, s.queues[DefaultQueue].store.Keys.Dead()).Val(); got != 1 {
		t.Fatalf("dead index size=%d", got)
	}
	if got := client.ZCard(ctx, s.queues[DefaultQueue].store.Keys.Scheduled()).Val(); got != 0 {
		t.Fatalf("scheduled index size=%d", got)
	}
	if got := client.ZCard(ctx, s.queues[DefaultQueue].store.Keys.Schedules()).Val(); got != 0 {
		t.Fatalf("cron index size=%d", got)
	}
}

func TestJanitorDeletesOnlyIdleConsumersWithoutPendingMessages(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.queues[DefaultQueue].store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.XGroupCreateConsumer(ctx, s.queues[DefaultQueue].store.Keys.Ready(), s.queues[DefaultQueue].store.Keys.Group(), "empty").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := mustBind(s, mustTask[string]("consumer.cleanup")).Enqueue(ctx, "payload"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.queues[DefaultQueue].store.ReadDeliveries(ctx, "pending", time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := s.queues[DefaultQueue].store.PruneStaleConsumers(ctx, time.Millisecond, 10); err != nil {
		t.Fatal(err)
	}
	consumers, err := client.XInfoConsumers(ctx, s.queues[DefaultQueue].store.Keys.Ready(), s.queues[DefaultQueue].store.Keys.Group()).Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(consumers) != 1 || consumers[0].Name != "pending" || consumers[0].Pending != 1 {
		t.Fatalf("consumers=%+v", consumers)
	}
}

func TestMaintenanceLeaseHasSingleOwner(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	ns := &store.Namespace{Client: client, Keys: store.NewNamespaceKeys(namespace)}
	store := store.Queue{Namespace: ns, Keys: ns.Keys.Queue(DefaultQueue)}
	first, err := store.AcquireMaintenanceLease(context.Background(), "one", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AcquireMaintenanceLease(context.Background(), "two", time.Second)
	if err != nil || !first || second {
		t.Fatalf("first=%v second=%v err=%v", first, second, err)
	}
	if renewed, renewErr := store.RenewMaintenanceLease(context.Background(), "two", time.Second); renewErr != nil || renewed {
		t.Fatalf("wrong owner renewed=%v err=%v", renewed, renewErr)
	}
	if renewed, renewErr := store.RenewMaintenanceLease(context.Background(), "one", time.Second); renewErr != nil || !renewed {
		t.Fatalf("owner renewed=%v err=%v", renewed, renewErr)
	}
	if err := store.ReleaseMaintenanceLease(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if value := client.Get(context.Background(), store.Keys.MaintenanceLease()).Val(); value != "one" {
		t.Fatalf("lease owner=%q", value)
	}
	if err := store.ReleaseMaintenanceLease(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerDeletionAtomicallyPreservesPendingWork(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("consumer-atomic-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	q := s.queues[DefaultQueue]
	if err := q.store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := mustBind(s, mustTask[int]("consumer-race")).Enqueue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	deliveries, err := q.store.ReadDeliveries(ctx, "candidate", time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("delivery=%v %v", deliveries, err)
	}
	removed, err := store.DeleteConsumer.Run(ctx, client, []string{q.store.Keys.Ready()}, q.store.Keys.Group(), "candidate").Int64()
	if err != nil || removed != 0 {
		t.Fatalf("consumer with new pending work deleted: %d %v", removed, err)
	}
	pending, err := client.XPending(ctx, q.store.Keys.Ready(), q.store.Keys.Group()).Result()
	if err != nil || pending.Count != 1 {
		t.Fatalf("pending work lost: %+v %v", pending, err)
	}
}
