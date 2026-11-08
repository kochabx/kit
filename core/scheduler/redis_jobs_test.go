package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

func TestRedisKeyPrefix(t *testing.T) {
	keys := store.NewNamespaceKeys("hermes").Queue(DefaultQueue)
	if want := "scheduler:{hermes}:v1:job:"; keys.JobPrefix() != want {
		t.Fatalf("key prefix = %q, want %q", keys.JobPrefix(), want)
	}
	if want := "scheduler:{hermes}:v1:queue:default:ready"; keys.Ready() != want {
		t.Fatalf("ready key = %q, want %q", keys.Ready(), want)
	}
}

func TestClaimReturnsAtomicExecutionSnapshot(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("start-snapshot-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	d := mustTask[string]("snapshot", WithRetry(NoRetry{}))
	job, err := mustBind(s, d).Enqueue(ctx, "binary\x00payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.queues[DefaultQueue].store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.queues[DefaultQueue].store.ReadDeliveries(ctx, "snapshot-reader", time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive: %v %v", deliveries, err)
	}
	// Warm the script cache so the counter distinguishes a normal EVALSHA from its cold fallback.
	if err := store.Claim.Load(ctx, client).Err(); err != nil {
		t.Fatal(err)
	}
	counter := &commandCounter{}
	client.AddHook(counter)
	started, err := s.queues[DefaultQueue].store.ClaimJob(ctx, deliveries[0], store.ClaimCommand{
		Token:              "token",
		Worker:             "worker",
		Duration:           3 * time.Second,
		ExpiredRetention:   time.Hour,
		CancelledRetention: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ID != job.ID ||
		started.State != JobRunning ||
		started.Attempt != 1 ||
		string(started.Payload) != string(job.Payload) ||
		started.Fingerprint != job.Fingerprint ||
		started.StartedAt.IsZero() {
		t.Fatalf("invalid snapshot: %+v", started)
	}
	if len(counter.commands) != 1 || counter.commands[0] != "evalsha" {
		t.Fatalf("start commands: %v", counter.commands)
	}
}

func TestBatchEnqueueIsolatesPipelineErrors(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("enqueue-errors-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	d := mustTask[int]("enqueue-errors")
	broken, err := mustBind(s, d).prepareJob(1)
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := mustBind(s, d).prepareJob(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, s.queues[DefaultQueue].store.Keys.Job(broken.ID), "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	results := s.queues[DefaultQueue].store.EnqueueBatch(ctx, []store.EnqueueRecord{broken, healthy})
	if results[0].Err == nil || results[1].Err != nil || results[1].Job == nil || results[1].Job.ID != healthy.ID {
		t.Fatalf("partial batch results=%+v", results)
	}
	job, err := s.Jobs().Get(ctx, healthy.ID)
	if err != nil || job.State != JobReady {
		t.Fatalf("healthy job=%+v err=%v", job, err)
	}
}

func TestRedisClockIsAnchoredToNamespace(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("clock-anchor-%d", time.Now().UnixNano())
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := store.Time.Load(ctx, client).Err(); err != nil {
		t.Fatal(err)
	}
	counter := &commandCounter{}
	client.AddHook(counter)
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil || now <= 0 {
		t.Fatalf("clock=%d err=%v", now, err)
	}
	if len(counter.commands) != 1 || counter.commands[0] != "evalsha" {
		t.Fatalf("clock was not routed by namespace script: %v", counter.commands)
	}
}
