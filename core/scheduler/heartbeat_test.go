package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestHeartbeatIsolatesPipelineErrors(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("heartbeat-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now()
	executions := make(map[string]*activeExecution)
	for _, id := range []string{"healthy", "cancelled", "lost", "broken", "finished"} {
		jobCtx, cancel := context.WithCancelCause(ctx)
		t.Cleanup(func() { cancel(nil) })
		execution := &activeExecution{
			token:     "token",
			ctx:       jobCtx,
			cancel:    cancel,
			nextRenew: now.Add(-time.Second),
			nextCheck: now.Add(-time.Second),
		}
		executions[id] = execution
		s.active.Store(id, execution)
		if err := client.HSet(ctx, s.queues[DefaultQueue].store.Keys.Job(id), "lease_token", "token", "lease_until", now.Add(time.Minute).UnixMilli(), "cancel_requested", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.HSet(ctx, s.queues[DefaultQueue].store.Keys.Job("cancelled"), "cancel_requested", 1).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, s.queues[DefaultQueue].store.Keys.Job("lost"), "lease_token", "other").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Del(ctx, s.queues[DefaultQueue].store.Keys.Job("broken")).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, s.queues[DefaultQueue].store.Keys.Job("broken"), "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	executions["finished"].finished.Store(true)
	counter := &commandCounter{}
	client.AddHook(counter)
	batch := heartbeatBatch{}
	batch.poll(s, ctx, now)
	if executions["healthy"].ctx.Err() != nil {
		t.Fatalf("healthy execution cancelled by another job's Redis error: %v", context.Cause(executions["healthy"].ctx))
	}
	if !errors.Is(context.Cause(executions["cancelled"].ctx), ErrJobCancelled) || !errors.Is(context.Cause(executions["lost"].ctx), ErrLeaseLost) || executions["broken"].ctx.Err() == nil {
		t.Fatal("heartbeat did not isolate cancellation/lease/error results")
	}
	if executions["finished"].ctx.Err() != nil {
		t.Fatal("finished execution was touched")
	}
	if counter.pipelines != 1 || len(counter.commands) != 4 {
		t.Fatalf("heartbeat pipelines=%d commands=%v", counter.pipelines, counter.commands)
	}
	counter.reset()
	batch.poll(s, ctx, now.Add(time.Millisecond))
	if len(counter.commands) != 0 {
		t.Fatalf("heartbeat sent requests before due: %v", counter.commands)
	}
}
