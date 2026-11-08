package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestStatsSeparateReadyAndPending(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.queues[DefaultQueue].store.EnsureConsumerGroup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := mustBind(s, mustTask[string]("stats.job")).Enqueue(context.Background(), "payload"); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background())
	if err != nil || stats.Ready != 1 || stats.Pending != 0 {
		t.Fatalf("before receive: stats=%+v err=%v", stats, err)
	}
	if _, err := s.queues[DefaultQueue].store.ReadDeliveries(context.Background(), "stats-consumer", time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	stats, err = s.Stats(context.Background())
	if err != nil || stats.Ready != 0 || stats.Pending != 1 {
		t.Fatalf("after receive: stats=%+v err=%v", stats, err)
	}
}

func TestDeadQueryFiltersScannedItems(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, taskName := range []string{"a", "b", "a", "b", "a"} {
		id := fmt.Sprintf("dead-%d", i)
		if err := client.HSet(ctx, s.queues[DefaultQueue].store.Keys.Job(id), "id", id, "queue", DefaultQueue, "task", taskName, "state", string(JobDead)).
			Err(); err != nil {
			t.Fatal(err)
		}
		if err := client.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Dead(), redis.Z{
			Score:  float64(i),
			Member: id,
		}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := mustQueue(s, DefaultQueue).DeadJobs(ctx, DeadQuery{Count: 2, TaskName: "a"})
	if err != nil || len(jobs.Items) != 3 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}
