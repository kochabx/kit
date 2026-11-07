package scheduler

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	kitvalidator "github.com/kochabx/kit/core/validator"
)

func TestConfigDefaultsAndValidation(t *testing.T) {
	c, err := prepareConfig(WorkerConfig{})
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	if c.Queues[0].Concurrency != 16 || c.MaxPayloadBytes != 1<<20 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	c.Execution.LeaseDuration = time.Second
	if _, err := prepareConfig(c); err == nil {
		t.Fatal("expected invalid lease duration")
	}

	for _, namespace := range []string{"-invalid", "invalid namespace", "调度器"} {
		if _, err := prepareConfig(WorkerConfig{Namespace: namespace}); err == nil {
			t.Fatalf("expected invalid namespace %q", namespace)
		}
	}
}

func TestConfigCanBeValidatedByDefaultValidator(t *testing.T) {
	type applicationConfig struct {
		Scheduler WorkerConfig `json:"scheduler"`
	}

	schedulerConfig, err := prepareConfig(WorkerConfig{Namespace: "jobs"})
	if err != nil {
		t.Fatalf("prepare scheduler config: %v", err)
	}
	c := applicationConfig{Scheduler: schedulerConfig}
	if err := kitvalidator.Validate.Struct(context.Background(), &c); err != nil {
		t.Fatalf("validate embedded scheduler config: %v", err)
	}
}

func TestRedisFailureBackoffUsesConfiguredBounds(t *testing.T) {
	b := newFailureBackoff(100*time.Millisecond, time.Second)
	for attempt := range 20 {
		d := b.NextDelay()
		if d < 50*time.Millisecond || d > time.Second {
			t.Fatalf("attempt %d: backoff %v outside jitter bounds", attempt, d)
		}
	}
	b.Reset()
	if d := b.NextDelay(); d < 50*time.Millisecond || d > 150*time.Millisecond {
		t.Fatalf("reset backoff=%v", d)
	}
}

func TestNestedConfigJSON(t *testing.T) {
	var config WorkerConfig
	if err := json.Unmarshal([]byte(`{"namespace":"nested","queues":[{"name":"default","concurrency":8}],"dispatch":{"batch":20},"maintenance":{"batch":10},"retention":{"succeeded":1000000000},"observability":{"buffer":128}}`), &config, json.WithUnmarshalers(json.UnmarshalFunc(func(data []byte, value *time.Duration) error {
		var nanos int64
		if err := json.Unmarshal(data, &nanos); err != nil {
			return err
		}
		*value = time.Duration(nanos)
		return nil
	}))); err != nil {
		t.Fatal(err)
	}
	config, err := prepareConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if config.Queues[0].Concurrency != 8 || config.Dispatch.Batch != 20 || config.Maintenance.Batch != 10 || config.Retention.Succeeded != time.Second || config.Observability.Buffer != 128 {
		t.Fatalf("nested config=%+v", config)
	}
	config.Dispatch.DrainLimit = 1
	if _, err := prepareConfig(config); err == nil {
		t.Fatal("invalid nested drain accepted")
	}
}

func TestHeartbeatConfigRequiresLeaseHeadroom(t *testing.T) {
	_, err := prepareConfig(WorkerConfig{Execution: ExecutionConfig{LeaseDuration: 3 * time.Second, LeaseRenewInterval: 2 * time.Second}})
	if err == nil {
		t.Fatal("renew interval without lease headroom accepted")
	}
}

func TestWorkerConsumesOnlyConfiguredQueues(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-subset-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	producer := queueTestScheduler(t, client, namespace, RoleDispatcher, "slow", "fast")
	workerConfig := producer.config
	workerConfig.Role = RoleWorker
	workerConfig.Execution.Queues = []string{"fast"}
	worker, err := newWorkerRuntime(client, workerConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	fast := mustTask[int]("task", WithQueue("fast"))
	slow := mustTask[int]("task", WithQueue("slow"))
	var count atomic.Int64
	if err := mustBind(worker, fast).Handle(func(context.Context, int) error { count.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := mustTask[int]("unknown", WithQueue("missing")).bind(worker.clientState); !errors.Is(err, ErrUnknownQueue) {
		t.Fatalf("unknown queue registration=%v", err)
	}
	runQueueTestScheduler(t, worker)
	slowJob, err := mustBind(producer, slow).Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mustBind(producer, fast).Enqueue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool { return count.Load() == 1 })
	current, err := producer.Jobs().Get(ctx, slowJob.ID)
	if err != nil || current.State != JobReady || current.Attempt != 0 || current.Queue != "slow" {
		t.Fatalf("unassigned queue consumed: %+v %v", current, err)
	}
	if _, err := mustBind(worker, slow).Enqueue(ctx, 2); err != nil {
		t.Fatalf("publishing to unconsumed queue: %v", err)
	}
	if err := producer.Jobs().Cancel(ctx, slowJob.ID); err != nil {
		t.Fatal(err)
	}
}
