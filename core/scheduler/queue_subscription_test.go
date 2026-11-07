package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

func TestDispatcherUsesExplicitQueueSubscriptions(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-dispatch-subset-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	base := queueTestScheduler(t, client, namespace, RoleDispatcher, "a", "b")
	config := base.config
	config.Dispatch.Queues = []string{"a"}
	s, err := newWorkerRuntime(client, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a, err := mustBind(s, mustTask[int]("task", WithQueue("a"))).Enqueue(ctx, 1, Delay(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	b, err := mustBind(s, mustTask[int]("task", WithQueue("b"))).Enqueue(ctx, 1, Delay(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	eventually(t, time.Second, func() bool { job, err := s.Jobs().Get(ctx, a.ID); return err == nil && job.State == JobReady })
	job, err := s.Jobs().Get(ctx, b.ID)
	if err != nil || job.State != JobScheduled {
		t.Fatalf("unassigned queue dispatched: %+v %v", job, err)
	}
}

func TestRecoveryStaysWithinSubscribedQueue(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-recovery-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	base := queueTestScheduler(t, client, namespace, RoleWorker, "a", "b")
	config := base.config
	config.Execution.Queues = []string{"a"}
	s, err := newWorkerRuntime(client, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var executed atomic.Int64
	a := mustTask[int]("task", WithQueue("a"))
	if err := mustBind(s, a).Handle(func(context.Context, int) error { executed.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	jobs := make(map[string]*Job)
	for _, name := range []string{"a", "b"} {
		q := s.queues[name]
		job, err := mustBind(s, mustTask[int]("task", WithQueue(name))).Enqueue(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		jobs[name] = job
		if err := q.store.EnsureConsumerGroup(ctx); err != nil {
			t.Fatal(err)
		}
		deliveries, err := q.store.ReadDeliveries(ctx, "abandoned", time.Millisecond, 1)
		if err != nil || len(deliveries) != 1 {
			t.Fatalf("receive=%v %v", deliveries, err)
		}
		if _, err := q.store.ClaimJob(ctx, deliveries[0], store.ClaimCommand{
			Token:              "old-token",
			Worker:             "abandoned",
			Duration:           3 * time.Second,
			ExpiredRetention:   time.Hour,
			CancelledRetention: time.Hour,
		}); err != nil {
			t.Fatal(err)
		}
		if err := client.HSet(ctx, s.namespace.Keys.Job(job.ID), "lease_until", 0).Err(); err != nil {
			t.Fatal(err)
		}
		// Advance the pending-message idle clock without making the test sleep for a lease.
		if err := client.Do(ctx, "XCLAIM", q.store.Keys.Ready(), q.store.Keys.Group(), "abandoned", 0, deliveries[0].MessageID, "IDLE", 60000).Err(); err != nil {
			t.Fatal(err)
		}
	}
	runQueueTestScheduler(t, s)
	eventually(t, 3*time.Second, func() bool {
		job, err := s.Jobs().Get(ctx, jobs["a"].ID)
		return err == nil && job.State == JobSucceeded
	})
	untouched, err := s.Jobs().Get(ctx, jobs["b"].ID)
	if err != nil || untouched.State != JobRunning || untouched.Attempt != 1 || executed.Load() != 1 {
		t.Fatalf("recovery crossed subscriptions: %+v %v executions=%d", untouched, err, executed.Load())
	}
}

func TestQueueSubscriptionsValidateAndPreserveEmpty(t *testing.T) {
	for _, config := range []WorkerConfig{
		{Queues: []QueueConfig{{Name: "a"}}, Execution: ExecutionConfig{Queues: []string{"missing"}}},
		{Queues: []QueueConfig{{Name: "a"}}, Dispatch: DispatchConfig{Queues: []string{"a", "a"}}},
	} {
		if _, err := prepareConfig(config); err == nil {
			t.Fatal("invalid queue subscription accepted")
		}
	}
	config, err := prepareConfig(WorkerConfig{Execution: ExecutionConfig{Queues: []string{}}, Dispatch: DispatchConfig{Queues: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if config.Execution.Queues == nil || config.Dispatch.Queues == nil {
		t.Fatal("explicit empty subscriptions became all queues")
	}
}
