package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSchedulerLifecycle(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 2}},
		Execution: ExecutionConfig{PollTimeout: 50 * time.Millisecond, LeaseDuration: 3 * time.Second},
		Dispatch:  DispatchConfig{Interval: 10 * time.Millisecond},
		Retention: RetentionConfig{Succeeded: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	type payload struct {
		Value string `json:"value"`
	}
	definition := mustTask[payload]("test.job", WithTimeout(time.Second), WithRetry(NoRetry{}))
	result := make(chan string, 1)
	if err := mustBind(s, definition).Handle(func(_ context.Context, p payload) error { result <- p.Value; return nil }); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := mustBind(s, definition).Enqueue(context.Background(), payload{Value: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got != "ok" {
			t.Fatalf("payload = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("job was not executed")
	}
	eventually(t, 2*time.Second, func() bool {
		current, err := s.Jobs().Get(context.Background(), job.ID)
		return err == nil && current.State == JobSucceeded
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestSeparateDispatcherAndWorkerRoles(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	dispatcher, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Role:      RoleDispatcher,
		Dispatch:  DispatchConfig{Interval: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Role:      RoleWorker,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second},
		Retention: RetentionConfig{Succeeded: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("roles.job", WithRetry(NoRetry{}))
	handled := make(chan struct{}, 1)
	if err := mustBind(worker, d).Handle(func(context.Context, string) error { handled <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- dispatcher.Run(ctx) }()
	go func() { done <- worker.Run(ctx) }()
	job, err := mustBind(dispatcher, d).Enqueue(context.Background(), "payload", Delay(30*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("separate worker did not execute job")
	}
	eventually(t, time.Second, func() bool {
		current, getErr := worker.Jobs().Get(context.Background(), job.ID)
		return getErr == nil && current.State == JobSucceeded
	})
	cancel()
	<-done
	<-done
}

func TestGracefulShutdownKeepsActiveJobLeasedUntilCompletion(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second, LeaseRenewInterval: 100 * time.Millisecond, CancellationCheckInterval: 50 * time.Millisecond, ShutdownTimeout: 2 * time.Second},
		Retention: RetentionConfig{Succeeded: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("shutdown.complete", WithTimeout(5*time.Second), WithRetry(NoRetry{}))
	started, release := make(chan struct{}), make(chan struct{})
	if err := mustBind(s, d).Handle(func(context.Context, string) error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	time.Sleep(250 * time.Millisecond) // crosses multiple renew intervals during shutdown
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err := s.Jobs().Get(context.Background(), job.ID)
	if err != nil || current.State != JobSucceeded {
		t.Fatalf("job=%+v err=%v", current, err)
	}
}

func TestShutdownTimeoutLeavesJobRecoverable(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace: namespace,
		Queues:    []QueueConfig{{Name: DefaultQueue, Concurrency: 1}},
		Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second, LeaseRenewInterval: 100 * time.Millisecond, CancellationCheckInterval: 50 * time.Millisecond, ShutdownTimeout: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("shutdown.timeout", WithTimeout(5*time.Second), WithRetry(NoRetry{}))
	started, release := make(chan struct{}), make(chan struct{})
	if err := mustBind(s, d).Handle(func(context.Context, string) error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := mustBind(s, d).Enqueue(context.Background(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	if err := <-done; !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("shutdown error=%v", err)
	}
	close(release)
	current, err := s.Jobs().Get(context.Background(), job.ID)
	if err != nil || current.State != JobRunning {
		t.Fatalf("job=%+v err=%v", current, err)
	}
}
