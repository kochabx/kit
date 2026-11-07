package scheduler

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

var errInjectedOutage = errors.New("injected Redis outage")

type outageHook struct {
	commandCounter
	offline atomic.Bool
}

func (h *outageHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.offline.Load() {
			return errInjectedOutage
		}
		return next(ctx, cmd)
	}
}
func (h *outageHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if h.offline.Load() {
			for _, cmd := range cmds {
				cmd.SetErr(errInjectedOutage)
			}
			return errInjectedOutage
		}
		return next(ctx, cmds)
	}
}

func TestTransientRedisFailurePreservesCauseAndResumes(t *testing.T) {
	client := testRedis(t)
	hook := &outageHook{}
	client.AddHook(hook)
	namespace := fmt.Sprintf("outage-resume-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, DefaultQueue)
	task, err := newTaskClient[int](s, "outage")
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan int, 1)
	if err := task.Handle(func(_ context.Context, v int) error { handled <- v; return nil }); err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	if _, err := task.Enqueue(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	hook.offline.Store(true)
	if _, err := task.Enqueue(context.Background(), 1); !errors.Is(err, errInjectedOutage) || errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("storage error: %v", err)
	}
	results := task.EnqueueBatch(context.Background(), []int{1, 2})
	for _, r := range results {
		if !errors.Is(r.Err, errInjectedOutage) {
			t.Fatalf("batch error: %v", r.Err)
		}
	}
	hook.offline.Store(false)
	if _, err := task.Enqueue(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-handled:
		if got != 42 {
			t.Fatalf("unexpected payload %d", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not resume")
	}
}

func TestFailedCompletionIsRecoveredAndOldTokenIsFenced(t *testing.T) {
	client := testRedis(t)
	hook := &outageHook{}
	client.AddHook(hook)
	namespace := fmt.Sprintf("completion-recovery-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, DefaultQueue)
	task, err := newTaskClient[int](s, "recover")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := task.Handle(func(context.Context, int) error {
		calls++
		if calls == 1 {
			hook.offline.Store(true)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := s.queues[DefaultQueue]
	if err := q.store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := task.Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := q.store.ReadDeliveries(ctx, "reader", time.Millisecond, 1)
	if err != nil || len(messages) != 1 {
		t.Fatalf("receive: %v %v", messages, err)
	}
	q.execute(ctx, "old-worker", messages[0])
	hook.offline.Store(false)
	oldToken, err := client.HGet(ctx, q.store.Keys.Job(job.ID), "lease_token").Result()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.Jobs().Get(ctx, job.ID)
	if err != nil || stored.State != JobRunning {
		t.Fatalf("uncommitted job: %+v %v", stored, err)
	}
	if err := client.HSet(ctx, q.store.Keys.Job(job.ID), "lease_until", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Do(ctx, "XCLAIM", q.store.Keys.Ready(), q.store.Keys.Group(), "reader", 0, messages[0].MessageID, "IDLE", 60000).Err(); err != nil {
		t.Fatal(err)
	}
	recovered := make(chan store.Delivery, 1)
	if _, err := q.recoverPending(ctx, recovered, "0-0"); err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 {
		t.Fatal("failed completion was not recoverable")
	}
	d := <-recovered
	replacement, err := q.store.ClaimJob(ctx, d, store.ClaimCommand{Token: "replacement", Worker: "new-worker", Duration: time.Minute, ExpiredRetention: time.Hour, CancelledRetention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.store.CompleteJob(ctx, messages[0], oldToken, time.Minute, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old token completed replacement: %v", err)
	}
	if replacement.Attempt != 2 {
		t.Fatalf("attempt=%d", replacement.Attempt)
	}
	if err := client.HSet(ctx, q.store.Keys.Job(job.ID), "lease_until", 0).Err(); err != nil {
		t.Fatal(err)
	}
	q.execute(ctx, "new-worker", d)
	stored, err = s.Jobs().Get(ctx, job.ID)
	if err != nil || stored.State != JobSucceeded || calls != 2 {
		t.Fatalf("recovered: %+v %v calls=%d", stored, err, calls)
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Pending != 0 || stats.Running != 0 {
		t.Fatalf("stats: %+v %v", stats, err)
	}
}

func TestInvalidArgumentsAreClassifiable(t *testing.T) {
	client := testRedis(t)
	for _, config := range []WorkerConfig{{Namespace: "bad namespace"}, {Queues: []QueueConfig{{Name: "bad queue"}}}, {Execution: ExecutionConfig{Queues: []string{"missing"}}}, {Execution: ExecutionConfig{LeaseDuration: 3 * time.Second, LeaseRenewInterval: 2 * time.Second}}} {
		if _, err := newWorkerRuntime(client, config); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("config %+v: %v", config, err)
		}
	}
	s, err := newWorkerRuntime(client, WorkerConfig{MaxPayloadBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := newTaskClient[string](s, "arguments")
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]EnqueueOption{{Delay(-1)}, {Unique("key", 0)}, {ExpiresAfter(-1)}} {
		if _, err := task.Enqueue(context.Background(), "x", options...); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("option error: %v", err)
		}
	}
	if _, err := task.Enqueue(context.Background(), "oversized payload"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("payload error: %v", err)
	}
	for _, spec := range []CronSpec{{Expression: "invalid"}, {Expression: "@every 1h", TimeZone: "invalid/zone"}} {
		if _, err := task.Schedule(context.Background(), "x", spec); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("cron error: %v", err)
		}
	}
}

func TestConnectionRefusalThenReconnect(t *testing.T) {
	base := testRedis(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := listener.Addr().String()
	listener.Close()
	var restored atomic.Bool
	client := redis.NewClient(&redis.Options{ContextTimeoutEnabled: true, Addr: base.Options().Addr, Password: base.Options().Password, MaxRetries: -1, DialTimeout: 100 * time.Millisecond, Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !restored.Load() {
			addr = refused
		}
		return (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ctx, network, addr)
	}})
	t.Cleanup(func() { _ = client.Close() })
	namespace := fmt.Sprintf("network-reconnect-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, base, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, DefaultQueue)
	task, err := newTaskClient[int](s, "network")
	if err != nil {
		t.Fatal(err)
	}
	_, err = task.Enqueue(context.Background(), 1)
	if _, ok := errors.AsType[*net.OpError](err); !ok {
		t.Fatalf("expected network cause, got %v", err)
	}
	restored.Store(true)
	done := make(chan struct{}, 1)
	if err := task.Handle(func(context.Context, int) error { done <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	if _, err := task.Enqueue(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not execute after connection restoration")
	}
}

func TestEncodingErrorRetainsCause(t *testing.T) {
	client := redis.NewClient(&redis.Options{ContextTimeoutEnabled: true, Addr: "127.0.0.1:0"})
	defer client.Close()
	s, err := newWorkerRuntime(client, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := newTaskClient[float64](s, "encoding")
	if err != nil {
		t.Fatal(err)
	}
	_, err = task.Enqueue(context.Background(), math.NaN())
	_, isEncoding := errors.AsType[*json.SemanticError](err)
	if !errors.Is(err, ErrInvalidArgument) || !isEncoding {
		t.Fatalf("encoding cause lost: %v", err)
	}
}
