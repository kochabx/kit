package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestRetry(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
		Dispatch:  DispatchConfig{Interval: 5 * time.Millisecond},
		Retention: RetentionConfig{Succeeded: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("retry.job", WithRetry(Exponential{
		MaxAttempts: 2,
		Initial:     10 * time.Millisecond,
		Max:         10 * time.Millisecond,
	}))
	var calls atomic.Int32
	if err := mustBind(s, d).Handle(func(context.Context, string) error {
		if calls.Add(1) == 1 {
			return errors.New("try again")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, err := s.Jobs().Get(context.Background(), job.ID)
		return err == nil && current.State == JobSucceeded && current.Attempt == 2
	})
	cancel()
	<-done
}

func TestPermanentFailureAndDeadRetry(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
		Dispatch:  DispatchConfig{Interval: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("dead.job")
	if err := mustBind(s, d).Handle(func(context.Context, string) error {
		return Permanent(errors.New("bad input"))
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, getErr := s.Jobs().Get(context.Background(), job.ID)
		return getErr == nil && current.State == JobDead
	})
	dead, err := mustQueue(s, DefaultQueue).DeadJobs(context.Background(), DeadQuery{Count: 10})
	if err != nil || len(dead.Items) != 1 {
		t.Fatalf("dead jobs = %v, %v", dead, err)
	}
	if err := s.Jobs().Retry(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.Jobs().Get(context.Background(), job.ID)
	if err != nil || (current.State != JobScheduled && current.State != JobReady) || current.Attempt != 0 {
		t.Fatalf("retried job = %+v, %v", current, err)
	}
}

func TestActiveLeaseCannotBeRecoveredOrAcknowledged(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Execution: ExecutionConfig{LeaseDuration: 3 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("lease.job")
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.queues[DefaultQueue].store.EnsureConsumerGroup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.queues[DefaultQueue].store.DispatchJobs(context.Background(), 10, s.config.Retention.Expired); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.queues[DefaultQueue].store.ReadDeliveries(context.Background(), "worker-a", 10*time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive: %v, %v", deliveries, err)
	}
	delivery := deliveries[0]
	if _, err := s.queues[DefaultQueue].store.ClaimJob(context.Background(), delivery, store.ClaimCommand{
		Token:              "token-a",
		Worker:             "worker-a",
		Duration:           3 * time.Second,
		ExpiredRetention:   s.config.Retention.Expired,
		CancelledRetention: s.config.Retention.Expired,
	}); err != nil {
		t.Fatal(err)
	}
	messages, _, err := client.XAutoClaim(context.Background(), &redis.XAutoClaimArgs{
		Stream:   s.queues[DefaultQueue].store.Keys.Ready(),
		Group:    s.queues[DefaultQueue].store.Keys.Group(),
		Consumer: "worker-b",
		MinIdle:  0,
		Start:    "0-0",
		Count:    1,
	}).Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("claim: %v, %v", messages, err)
	}
	claimed := delivery
	claimed.MessageID = messages[0].ID
	if _, err := s.queues[DefaultQueue].store.ClaimJob(context.Background(), claimed, store.ClaimCommand{
		Token:              "token-b",
		Worker:             "worker-b",
		Duration:           3 * time.Second,
		ExpiredRetention:   s.config.Retention.Expired,
		CancelledRetention: s.config.Retention.Expired,
	}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("start active lease: %v", err)
	}
	pending, err := client.XPending(context.Background(), s.queues[DefaultQueue].store.Keys.Ready(), s.queues[DefaultQueue].store.Keys.Group()).Result()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Count != 1 {
		t.Fatalf("active delivery was acknowledged, pending=%d job=%s", pending.Count, job.ID)
	}
}

func TestSucceededJobIsDeletedByDefault(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("success.delete", WithRetry(NoRetry{}))
	handled := make(chan struct{}, 1)
	if err := mustBind(s, d).Handle(func(context.Context, string) error {
		handled <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload", Unique("success:key", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("job was not handled")
	}
	eventually(t, time.Second, func() bool {
		_, getErr := s.Jobs().Get(context.Background(), job.ID)
		return errors.Is(getErr, ErrNotFound)
	})
	duplicate, err := mustBind(s, d).Enqueue(context.Background(), "payload", Unique("success:key", time.Minute))
	if duplicate != nil || !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunningJobCancellation(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
		Dispatch:  DispatchConfig{Interval: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("cancel.running", WithTimeout(10*time.Second), WithRetry(NoRetry{}))
	started := make(chan struct{})
	if err := mustBind(s, d).Handle(func(ctx context.Context, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}
	if err := s.Jobs().Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, getErr := s.Jobs().Get(context.Background(), job.ID)
		return getErr == nil && current.State == JobCancelled
	})
}

func TestRemoteRunningJobCancellation(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	worker, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Role:      RoleWorker,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{
			PollTimeout:               20 * time.Millisecond,
			LeaseDuration:             3 * time.Second,
			LeaseRenewInterval:        time.Second,
			CancellationCheckInterval: 20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	producer, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Role: RoleDispatcher})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("cancel.remote", WithTimeout(10*time.Second), WithRetry(NoRetry{}))
	started := make(chan struct{})
	if err := mustBind(worker, d).Handle(func(ctx context.Context, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() { cancel(); <-done }()
	job, err := mustBind(producer, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}
	if err := producer.Jobs().Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		current, getErr := producer.Jobs().Get(context.Background(), job.ID)
		return getErr == nil && current.State == JobCancelled
	})
}

func TestCancellationWinsOverFailureAndRecovery(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("cancel-transition-%d", time.Now().UnixNano())
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
	task := mustBind(s, mustTask[int]("cancel-transition"))
	for _, mode := range []string{"failure", "recovery"} {
		job, err := task.Enqueue(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		deliveries, err := q.store.ReadDeliveries(ctx, "reader", time.Millisecond, 1)
		if err != nil || len(deliveries) != 1 {
			t.Fatalf("delivery=%v %v", deliveries, err)
		}
		d := deliveries[0]
		command := store.ClaimCommand{
			Token:              "token",
			Worker:             "worker",
			Duration:           3 * time.Second,
			ExpiredRetention:   time.Hour,
			CancelledRetention: time.Hour,
		}
		if _, err := q.store.ClaimJob(ctx, d, command); err != nil {
			t.Fatal(err)
		}
		if err := s.Jobs().Cancel(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
		if mode == "failure" {
			err := q.store.FailJob(ctx, d, "token", store.FailureCommand{
				Message:            "transient",
				Retry:              true,
				Delay:              time.Second,
				DeadRetention:      time.Hour,
				CancelledRetention: time.Hour,
			})
			if !errors.Is(err, ErrJobCancelled) {
				t.Fatalf("failure retried cancelled job: %v", err)
			}
		} else {
			if err := client.HSet(ctx, s.namespace.Keys.Job(job.ID), "lease_until", 0).Err(); err != nil {
				t.Fatal(err)
			}
			command.Token = "new-token"
			if _, err := q.store.ClaimJob(ctx, d, command); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("recovery acquired cancelled job: %v", err)
			}
		}
		current, err := s.Jobs().Get(ctx, job.ID)
		if err != nil || current.State != JobCancelled || current.Attempt != 1 {
			t.Fatalf("cancelled job=%+v err=%v", current, err)
		}
	}
	stats, err := mustQueue(s, DefaultQueue).Stats(ctx)
	if err != nil || stats.Pending != 0 || stats.Running != 0 || stats.Scheduled != 0 || stats.Dead != 0 {
		t.Fatalf("cancelled jobs left in active indexes: %+v %v", stats, err)
	}
}
