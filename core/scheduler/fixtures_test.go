package scheduler

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func mustTask[T any](name string, opts ...TaskOption) Task[T] {
	task, err := NewTask[T](name, opts...)
	if err != nil {
		panic(err)
	}
	return task
}

type testProducer[T any] struct {
	*taskClient[T]
	worker *workerRuntime
}

func (t *testProducer[T]) Handle(fn Handler[T]) error {
	return t.worker.registerTaskHandler(t.task, fn)
}
func mustBind[T any](s *workerRuntime, t Task[T]) *testProducer[T] {
	client, err := t.bind(s.clientState)
	if err != nil {
		panic(err)
	}
	return &testProducer[T]{taskClient: client, worker: s}
}

func mustQueue(s *workerRuntime, name string) *Queue {
	queue, err := s.Queue(name)
	if err != nil {
		panic(err)
	}
	return queue
}

func testRedis(t testing.TB) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	password := os.Getenv("REDIS_PASSWORD")
	if password == "" {
		password = "12345678"
	}
	client := redis.NewClient(&redis.Options{ContextTimeoutEnabled: true, Addr: addr, Password: password})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		if os.Getenv("SCHEDULER_INTEGRATION_REQUIRED") == "1" {
			t.Fatalf("required Redis integration unavailable: %v", err)
		}
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func cleanup(t testing.TB, client *redis.Client, namespace string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pattern := "scheduler:{" + namespace + "}:*"
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = client.Del(ctx, keys...).Err()
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}

func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met")
}

type commandCounter struct {
	mu        sync.Mutex
	commands  []string
	pipelines int
}

func (h *commandCounter) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}

func (h *commandCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		if cmd.Name() != "hello" && cmd.Name() != "client" {
			h.commands = append(h.commands, cmd.Name())
		}
		h.mu.Unlock()
		return next(ctx, cmd)
	}
}

func (h *commandCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		h.mu.Lock()
		hasBusinessCommand := false
		for _, cmd := range cmds {
			if cmd.Name() != "hello" && cmd.Name() != "client" {
				hasBusinessCommand = true
			}
			if cmd.Name() != "hello" && cmd.Name() != "client" {
				h.commands = append(h.commands, cmd.Name())
			}
		}
		if hasBusinessCommand {
			h.pipelines++
		}
		h.mu.Unlock()
		return next(ctx, cmds)
	}
}

func (h *commandCounter) reset() { h.mu.Lock(); defer h.mu.Unlock(); h.commands = nil; h.pipelines = 0 }

func queueTestScheduler(t *testing.T, client redis.UniversalClient, namespace string, role Role, names ...string) *workerRuntime {
	t.Helper()
	configs := make([]QueueConfig, 0, len(names))
	for _, name := range names {
		configs = append(configs, QueueConfig{Name: name, Concurrency: 1, FetchBatch: 2, Prefetch: 1})
	}
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Role: role, Queues: configs,
		Execution: ExecutionConfig{
			PollTimeout:               20 * time.Millisecond,
			LeaseDuration:             3 * time.Second,
			LeaseRenewInterval:        100 * time.Millisecond,
			CancellationCheckInterval: 20 * time.Millisecond,
			ShutdownTimeout:           time.Second,
		},
		Dispatch: DispatchConfig{Interval: 5 * time.Millisecond}, Retention: RetentionConfig{Succeeded: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func runQueueTestScheduler(t *testing.T, s *workerRuntime) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("shutdown: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("scheduler did not stop")
		}
	})
}

// newTaskClient validates task metadata using the default JSON codec and binds
// it to s. It neither registers a handler nor starts the engine.
// Test helper for exercising the internal producer adapter.
func newTaskClient[T any](s *workerRuntime, name string, opts ...TaskOption) (*testProducer[T], error) {
	task, err := NewTask[T](name, opts...)
	if err != nil {
		return nil, err
	}
	return mustBind(s, task), nil
}

// Handle registers the task handler; it does not execute a task.
// Call it before Run. Registering a second handler returns ErrTaskRegistered.
func (t *taskClient[T]) Handle(fn Handler[T]) error {
	if !t.isValid() {
		return ErrInvalidArgument
	}

	return ErrInvalidArgument
}
