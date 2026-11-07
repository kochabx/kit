package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

type failCompletionHook struct {
	commandCounter
	err error
}

func (h *failCompletionHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "evalsha" && len(args) > 1 && args[1] == store.Complete.Hash() {
			return h.err
		}
		return next(ctx, cmd)
	}
}

func TestFinishedEventSeparatesCommitFailure(t *testing.T) {
	client := testRedis(t)
	commitErr := errors.New("completion storage unavailable")
	client.AddHook(&failCompletionHook{err: commitErr})
	namespace := fmt.Sprintf("commit-event-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	events := make(chan Event, 8)
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Observability: ObservabilityConfig{Observer: ObserverFunc(func(_ context.Context, event Event) { events <- event })}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bound := mustBind(s, mustTask[int]("commit"))
	if err := bound.Handle(func(context.Context, int) error { return nil }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	job, err := bound.Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	q := s.queues[DefaultQueue]
	if err := q.store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, err := q.store.ReadDeliveries(ctx, "reader", time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive: %v %v", deliveries, err)
	}
	q.execute(ctx, "worker", deliveries[0])
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind != EventFinished {
				continue
			}
			if event.Err != nil || !errors.Is(event.CommitErr, commitErr) {
				t.Fatalf("event: %+v", event)
			}
			stored, err := s.Jobs().Get(ctx, job.ID)
			if err != nil || stored.State != JobRunning {
				t.Fatalf("job=%+v err=%v", stored, err)
			}
			return
		case <-timer.C:
			t.Fatal("missing finished event")
		}
	}
}

func TestRetryPolicyPanicMarksJobDead(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("policy-panic-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bound := mustBind(s, mustTask[int]("panic", WithRetry(panicRetry{})))
	if err := bound.Handle(func(context.Context, int) error { return errors.New("temporary") }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	job, err := bound.Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	q := s.queues[DefaultQueue]
	if err := q.store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, err := q.store.ReadDeliveries(ctx, "reader", time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive: %v %v", deliveries, err)
	}
	q.execute(ctx, "worker", deliveries[0])
	stored, err := s.Jobs().Get(ctx, job.ID)
	if err != nil || stored.State != JobDead {
		t.Fatalf("job=%+v err=%v", stored, err)
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Pending != 0 || stats.Running != 0 || stats.Dead != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}
