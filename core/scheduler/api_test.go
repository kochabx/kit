package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

func TestTaskConstructionRejectsInvalidStrategies(t *testing.T) {
	var codec *jsonCodec[int]
	if _, err := NewTaskWithCodec("nil-codec", codec); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("typed nil codec: %v", err)
	}
	if _, err := NewTask[int]("nil-retry", WithRetry((*Exponential)(nil))); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("typed nil retry: %v", err)
	}
	if _, err := NewTask[int]("invalid name"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid task name: %v", err)
	}
	if _, err := NewTask[int]("invalid-timeout", WithTimeout(0)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid timeout: %v", err)
	}
	var client *redis.Client
	if _, err := newWorkerRuntime(client, WorkerConfig{}); err == nil {
		t.Fatal("typed nil Redis client accepted")
	}
}

func TestZeroValueResourceHandlesReturnErrors(t *testing.T) {
	ctx := context.Background()
	var task Task[int]
	if _, err := task.bind(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	var client taskClient[int]
	if _, err := client.Enqueue(ctx, 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if err := client.Handle(func(context.Context, int) error { return nil }); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := client.Schedule(ctx, 1, CronSpec{Expression: "@every 1h"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	results := client.EnqueueBatch(ctx, []int{1, 2})
	for _, result := range results {
		if !errors.Is(result.Err, ErrInvalidArgument) {
			t.Fatal("invalid batch client succeeded")
		}
	}
	var jobs Jobs
	if _, err := jobs.Get(ctx, "id"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if err := jobs.Delete(ctx, "id"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	var schedules Schedules
	if err := schedules.Pause(ctx, "id"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	var queue Queue
	if _, err := queue.Stats(ctx); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
}

func TestBoundTaskClientLifecycleAndBorrowedRedis(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("task-client-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	task, err := NewTask[int]("bound")
	if err != nil {
		t.Fatal(err)
	}
	bound := mustBind(s, task)
	handler := func(context.Context, int) error { return nil }
	if err := bound.Handle(handler); err != nil {
		t.Fatal(err)
	}
	if err := bound.Handle(handler); !errors.Is(err, ErrTaskRegistered) {
		t.Fatalf("duplicate handler=%v", err)
	}
	if _, err := s.Queue(""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("empty queue selected implicitly")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Enqueue(ctx, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("enqueue after Close: %v", err)
	}
	if _, err := task.bind(s.clientState); !errors.Is(err, ErrClosed) {
		t.Fatalf("bind after Close: %v", err)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("scheduler closed borrowed Redis: %v", err)
	}
}

func TestJobDeletionRejectsActiveAndPreservesUniqueWindow(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("terminal-delete-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	bound := mustBind(s, mustTask[int]("delete"))
	job, err := bound.Enqueue(ctx, 1, Unique("business", time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs().Delete(ctx, job.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("active deletion=%v", err)
	}
	current, err := s.Jobs().Get(ctx, job.ID)
	if err != nil || current.State != JobReady {
		t.Fatalf("active job mutated=%+v %v", current, err)
	}
	if err := s.Jobs().Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs().Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Jobs().Get(ctx, job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("terminal job not deleted")
	}
	if duplicate, err := bound.Enqueue(ctx, 2, Unique("business", time.Hour)); !errors.Is(err, ErrDuplicate) || duplicate != nil {
		t.Fatalf("delete removed uniqueness window: %+v %v", duplicate, err)
	}
}

func TestPositiveSubMillisecondUniqueTTLIsNotPermanent(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("tiny-ttl-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := mustBind(s, mustTask[int]("ttl")).Enqueue(ctx, 1, Unique("tiny", time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	ttl, err := client.PTTL(ctx, s.namespace.Keys.Unique("tiny")).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl == -time.Nanosecond {
		t.Fatal("positive TTL became permanent")
	}
	eventually(t, time.Second, func() bool { return client.Exists(ctx, s.namespace.Keys.Unique("tiny")).Val() == 0 })
}

func TestObserverConcurrentPublishAndCloseAccountsForEvents(t *testing.T) {
	var observed atomic.Uint64
	bus := newEventBus(ObservabilityConfig{
		Buffer:   64,
		Logger:   log.Global(),
		Observer: ObserverFunc(func(context.Context, Event) { observed.Add(1) }),
	}, time.Second)
	const writers = 8
	const count = 1000
	var writersDone sync.WaitGroup
	for range writers {
		writersDone.Go(func() {
			for range count {
				bus.publish(context.Background(), Event{Kind: EventEnqueued, Queue: "mail"})
			}
		})
	}
	bus.close()
	writersDone.Wait()
	bus.close()
	if total := observed.Load() + bus.droppedCount(); total != writers*count {
		t.Fatalf("unaccounted events: processed=%d dropped=%d", observed.Load(), bus.droppedCount())
	}
}

func TestObserverPanicDoesNotStopLaterEvents(t *testing.T) {
	var received atomic.Uint64
	bus := newEventBus(ObservabilityConfig{
		Buffer: 4,
		Logger: log.Global(),
		Observer: ObserverFunc(func(_ context.Context, event Event) {
			if event.Kind == EventStarted {
				panic("observer failure")
			}
			received.Add(1)
		}),
	}, time.Second)
	bus.publish(context.Background(), Event{Kind: EventStarted})
	bus.publish(context.Background(), Event{Kind: EventFinished})
	bus.close()
	if received.Load() != 1 || bus.droppedCount() != 1 {
		t.Fatalf("observer stopped: received=%d dropped=%d", received.Load(), bus.droppedCount())
	}
}

func TestObserverReportsQueueAndTaskLifecycle(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("observed-task-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	events := make(chan Event, 8)
	s, err := newWorkerRuntime(client, WorkerConfig{
		Namespace:     namespace,
		Queues:        []QueueConfig{{Name: "mail", Concurrency: 1}},
		Execution:     ExecutionConfig{PollTimeout: 20 * time.Millisecond},
		Observability: ObservabilityConfig{Observer: ObserverFunc(func(_ context.Context, event Event) { events <- event })},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := mustBind(s, mustTask[int]("mail.send", WithQueue("mail")))
	if err := task.Handle(func(context.Context, int) error { return nil }); err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	job, err := task.Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[EventKind]bool{}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(seen) < 3 {
		select {
		case event := <-events:
			if event.JobID != job.ID || event.Queue != "mail" || event.TaskName != "mail.send" {
				t.Fatalf("wrong event=%+v", event)
			}
			seen[event.Kind] = true
		case <-deadline.C:
			t.Fatalf("missing lifecycle events: %v", seen)
		}
	}
}

func TestDeadQueryPropagatesCorruptStorageErrors(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("query-error-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	q := s.queues[DefaultQueue]
	if err := client.Set(ctx, s.namespace.Keys.Job("corrupt"), "not-a-hash", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, q.store.Keys.Dead(), redis.Z{
		Score:  float64(time.Now().UnixMilli()),
		Member: "corrupt",
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := mustQueue(s, DefaultQueue).DeadJobs(ctx, DeadQuery{Count: 10}); err == nil {
		t.Fatal("storage error was silently hidden")
	}
}
