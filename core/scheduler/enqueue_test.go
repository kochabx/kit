package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestUniqueAndCancel(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("unique.job")
	first, err := mustBind(s, d).Enqueue(context.Background(), "one", Delay(time.Hour), Unique("account:1", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, err := mustBind(s, d).Enqueue(context.Background(), "two", Delay(time.Hour), Unique("account:1", time.Minute))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate, got %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate id = %q, want %q", second.ID, first.ID)
	}
	if err := s.Jobs().Cancel(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.Jobs().Get(context.Background(), first.ID)
	if err != nil || current.State != JobCancelled {
		t.Fatalf("cancelled job: %+v, %v", current, err)
	}
}

func TestBatchEnqueue(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[int]("batch.job")
	results := mustBind(s, d).EnqueueBatch(context.Background(), []int{1, 2, 3})
	for i, result := range results {
		if result.Err != nil || result.Job == nil || result.Job.State != JobReady {
			t.Fatalf("result %d: %+v", i, result)
		}
	}
	stats, err := s.Stats(context.Background())
	if err != nil || stats.Ready != 3 || stats.Scheduled != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestReadyStreamExecutesAllJobs(t *testing.T) {
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
	d := mustTask[int]("stream.job", WithRetry(NoRetry{}))
	handled := make(chan int, 3)
	if err := mustBind(s, d).Handle(func(_ context.Context, v int) error { handled <- v; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	jobs := make([]*Job, 0, 3)
	for i := range 3 {
		job, enqueueErr := mustBind(s, d).Enqueue(context.Background(), i)
		if enqueueErr != nil {
			t.Fatal(enqueueErr)
		}
		jobs = append(jobs, job)
	}
	for range 3 {
		select {
		case <-handled:
		case <-time.After(3 * time.Second):
			t.Fatal("ready job not executed")
		}
	}
	for _, job := range jobs {
		eventually(t, time.Second, func() bool {
			current, getErr := s.Jobs().Get(context.Background(), job.ID)
			return getErr == nil && current.State == JobSucceeded
		})
	}
	cancel()
	<-done
}

func TestSubmittedRetryPolicySurvivesWorkerDefaults(t *testing.T) {
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
	producer := mustTask[string]("attempt.job", WithRetry(Exponential{
		MaxAttempts: 2,
		Initial:     5 * time.Millisecond,
		Max:         5 * time.Millisecond,
	}))
	worker := mustTask[string]("attempt.job", WithRetry(Exponential{
		MaxAttempts: 10,
		Initial:     5 * time.Millisecond,
		Max:         5 * time.Millisecond,
	}))
	if err := mustBind(s, worker).Handle(func(context.Context, string) error {
		return errors.New("always fails")
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	job, err := mustBind(s, producer).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, getErr := s.Jobs().Get(context.Background(), job.ID)
		return getErr == nil && current.State == JobDead && current.Attempt == 2 && current.FailureCount == 2 && current.LastError == "always fails"
	})
}

func TestUniqueKeyDoesNotDependOnJobLifetime(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := mustTask[string]("unique.repair")
	first, err := mustBind(s, d).Enqueue(context.Background(), "one", Unique("key", time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Del(context.Background(), s.queues[DefaultQueue].store.Keys.Job(first.ID)).Err(); err != nil {
		t.Fatal(err)
	}
	second, err := mustBind(s, d).Enqueue(context.Background(), "two", Unique("key", time.Hour))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate, got job=%+v err=%v", second, err)
	}
	if second != nil {
		t.Fatalf("duplicate returned deleted job: %+v", second)
	}
}

func TestImmediateJobDoesNotRequireDispatcher(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Role:      RoleWorker,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("immediate.fast", WithRetry(NoRetry{}))
	handled := make(chan struct{}, 1)
	if err := mustBind(s, d).Handle(func(context.Context, string) error {
		handled <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	if job.State != JobReady {
		t.Fatalf("initial state=%s", job.State)
	}
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("immediate job waited for dispatcher")
	}
	cancel()
	<-done
}

func TestDelayUsesRedisClock(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.Time(context.Background()).Result()
	if err != nil {
		t.Fatal(err)
	}
	job, err := mustBind(s, mustTask[string]("delay.clock")).Enqueue(context.Background(), "payload", Delay(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.Time(context.Background()).Result()
	if err != nil {
		t.Fatal(err)
	}
	lower := time.UnixMilli(before.Add(time.Minute).UnixMilli())
	upper := time.UnixMilli(after.Add(time.Minute).UnixMilli())
	if job.RunAt.Before(lower) || job.RunAt.After(upper) {
		t.Fatalf("run_at %v is not based on Redis interval [%v, %v]", job.RunAt, before, after)
	}
}

func TestCancelledReadyMessageIsDeletedFromStream(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Role:      RoleWorker,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("cancel.ready")
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs().Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	eventually(t, time.Second, func() bool {
		return client.XLen(context.Background(), s.queues[DefaultQueue].store.Keys.Ready()).Val() == 0
	})
	cancel()
	<-done
}

func TestQueuedJobExpiresBeforeDispatch(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	job, err := mustBind(s, mustTask[string]("job.expires")).Enqueue(context.Background(), "payload", Delay(time.Hour), ExpiresAfter(5*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := s.queues[DefaultQueue].store.DispatchJobs(context.Background(), 10, time.Hour); err != nil {
		t.Fatal(err)
	}
	current, err := s.Jobs().Get(context.Background(), job.ID)
	if err != nil || current.State != JobExpired {
		t.Fatalf("job=%+v err=%v", current, err)
	}
}
