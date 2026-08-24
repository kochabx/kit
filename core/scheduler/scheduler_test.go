package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	kitvalidator "github.com/kochabx/kit/core/validator"
)

func TestConfigDefaultsAndValidation(t *testing.T) {
	c, err := prepareConfig(Config{})
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	if c.Concurrency != 16 || c.MaxPayloadBytes != 1<<20 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	c.LeaseDuration = time.Second
	if _, err := prepareConfig(c); err == nil {
		t.Fatal("expected invalid lease duration")
	}

	for _, namespace := range []string{"-invalid", "invalid namespace", "调度器"} {
		if _, err := prepareConfig(Config{Namespace: namespace}); err == nil {
			t.Fatalf("expected invalid namespace %q", namespace)
		}
	}
}

func TestConfigCanBeValidatedByDefaultValidator(t *testing.T) {
	type applicationConfig struct {
		Scheduler Config `json:"scheduler"`
	}

	schedulerConfig, err := prepareConfig(Config{Namespace: "jobs"})
	if err != nil {
		t.Fatalf("prepare scheduler config: %v", err)
	}
	c := applicationConfig{Scheduler: schedulerConfig}
	if err := kitvalidator.Validate.Struct(context.Background(), &c); err != nil {
		t.Fatalf("validate embedded scheduler config: %v", err)
	}
}

func TestExponential(t *testing.T) {
	p := Exponential{MaxAttempts: 4, Initial: time.Second, Max: 3 * time.Second, Multiplier: 2}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	for attempt, expected := range want {
		got, retry := p.NextDelay(attempt+1, errors.New("failed"))
		if !retry || got != expected {
			t.Fatalf("attempt %d: got %v, %v", attempt+1, got, retry)
		}
	}
	if _, retry := p.NextDelay(4, errors.New("failed")); retry {
		t.Fatal("expected retry budget to be exhausted")
	}
}

func TestRedisFailureBackoffUsesConfiguredBounds(t *testing.T) {
	b := newFailureBackoff(100*time.Millisecond, time.Second)
	for attempt := range 20 {
		d := b.Next()
		if d < 50*time.Millisecond || d > 1500*time.Millisecond {
			t.Fatalf("attempt %d: backoff %v outside jitter bounds", attempt, d)
		}
	}
	b.Reset()
	if d := b.Next(); d < 50*time.Millisecond || d > 150*time.Millisecond {
		t.Fatalf("reset backoff=%v", d)
	}
}

func TestSchedulerLifecycle(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 2, DispatchInterval: 10 * time.Millisecond, PollTimeout: 50 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	type payload struct {
		Value string `json:"value"`
	}
	definition := Define[payload]("test.job", WithTimeout(time.Second), WithRetry(NoRetry{}))
	result := make(chan string, 1)
	if err := Handle(s, definition, func(_ context.Context, p payload) error { result <- p.Value; return nil }); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := Enqueue(s, context.Background(), definition, payload{Value: "ok"})
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
		current, err := s.Get(context.Background(), job.ID)
		return err == nil && current.State == StateSucceeded
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

func TestUniqueAndCancel(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("unique.job")
	first, err := Enqueue(s, context.Background(), d, "one", Delay(time.Hour), Unique("account:1", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Enqueue(s, context.Background(), d, "two", Delay(time.Hour), Unique("account:1", time.Minute))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate, got %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate id = %q, want %q", second.ID, first.ID)
	}
	if err := s.Cancel(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.Get(context.Background(), first.ID)
	if err != nil || current.State != StateCancelled {
		t.Fatalf("cancelled job: %+v, %v", current, err)
	}
}

func TestRetry(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, DispatchInterval: 5 * time.Millisecond, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("retry.job", WithRetry(Exponential{MaxAttempts: 2, Initial: 10 * time.Millisecond, Max: 10 * time.Millisecond}))
	var calls atomic.Int32
	if err := Handle(s, d, func(context.Context, string) error {
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
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, err := s.Get(context.Background(), job.ID)
		return err == nil && current.State == StateSucceeded && current.Attempt == 2
	})
	cancel()
	<-done
}

func TestCronCreatesIndependentJob(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("cron.job")
	schedule, err := ScheduleCron(s, context.Background(), d, "payload", "@every 1s", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Second).UnixMilli()
	if err := client.HSet(context.Background(), s.store.keys.schedule(schedule.ID), "next_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(context.Background(), s.store.keys.schedules(), redis.Z{Score: float64(due), Member: schedule.ID}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatchSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids, err := client.ZRange(context.Background(), s.store.keys.scheduled(), 0, -1).Result()
	if err != nil || len(ids) != 1 {
		t.Fatalf("cron instances = %v, %v", ids, err)
	}
	if ids[0] != schedule.ID+":"+fmt.Sprint(due) {
		t.Fatalf("instance id = %q", ids[0])
	}
	if err := s.dispatchSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count := client.ZCard(context.Background(), s.store.keys.scheduled()).Val(); count != 1 {
		t.Fatalf("duplicate cron occurrence: %d", count)
	}
}

func TestPermanentFailureAndDeadRetry(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, DispatchInterval: 5 * time.Millisecond, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("dead.job")
	if err := Handle(s, d, func(context.Context, string) error { return Permanent(errors.New("bad input")) }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, getErr := s.Get(context.Background(), job.ID)
		return getErr == nil && current.State == StateDead
	})
	dead, err := s.DeadJobs(context.Background(), 0, 10)
	if err != nil || len(dead) != 1 {
		t.Fatalf("dead jobs = %v, %v", dead, err)
	}
	if err := s.RetryDead(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.Get(context.Background(), job.ID)
	if err != nil || (current.State != StateScheduled && current.State != StateReady) || current.Attempt != 0 {
		t.Fatalf("retried job = %+v, %v", current, err)
	}
}

func TestActiveLeaseCannotBeRecoveredOrAcknowledged(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("lease.job")
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.ensureGroups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.dispatch(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.store.receive(context.Background(), "worker-a", 10*time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive: %v, %v", deliveries, err)
	}
	delivery := deliveries[0]
	if _, err := s.store.start(context.Background(), delivery, "token-a", "worker-a", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	messages, _, err := client.XAutoClaim(context.Background(), &redis.XAutoClaimArgs{Stream: s.store.keys.ready(), Group: s.store.keys.group(), Consumer: "worker-b", MinIdle: 0, Start: "0-0", Count: 1}).Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("claim: %v, %v", messages, err)
	}
	claimed := delivery
	claimed.messageID = messages[0].ID
	if _, err := s.store.start(context.Background(), claimed, "token-b", "worker-b", 3*time.Second); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("start active lease: %v", err)
	}
	pending, err := client.XPending(context.Background(), s.store.keys.ready(), s.store.keys.group()).Result()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Count != 1 {
		t.Fatalf("active delivery was acknowledged, pending=%d job=%s", pending.Count, job.ID)
	}
}

func TestBatchEnqueue(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[int]("batch.job")
	results := BatchEnqueue(s, context.Background(), d, []int{1, 2, 3})
	for i, result := range results {
		if result.Err != nil || result.Job == nil || result.Job.State != StateReady {
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
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, DispatchInterval: 5 * time.Millisecond, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[int]("stream.job", WithRetry(NoRetry{}))
	handled := make(chan int, 3)
	if err := Handle(s, d, func(_ context.Context, v int) error { handled <- v; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	jobs := make([]*Job, 0, 3)
	for i := range 3 {
		job, enqueueErr := Enqueue(s, context.Background(), d, i)
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
			current, getErr := s.Get(context.Background(), job.ID)
			return getErr == nil && current.State == StateSucceeded
		})
	}
	cancel()
	<-done
}

func TestDefinitionMismatchIsRejected(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, DispatchInterval: 5 * time.Millisecond, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	producer := Define[string]("attempt.job", WithRetry(Exponential{MaxAttempts: 2, Initial: 5 * time.Millisecond, Max: 5 * time.Millisecond}))
	worker := Define[string]("attempt.job", WithRetry(Exponential{MaxAttempts: 10, Initial: 5 * time.Millisecond, Max: 5 * time.Millisecond}))
	if err := Handle(s, worker, func(context.Context, string) error { return errors.New("always fails") }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	job, err := Enqueue(s, context.Background(), producer, "payload")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, getErr := s.Get(context.Background(), job.ID)
		return getErr == nil && current.State == StateDead && current.Attempt == 1 && current.LastError == ErrDefinitionMismatch.Error()
	})
}

func TestOrphanedUniqueKeyIsRepaired(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := Define[string]("unique.repair")
	first, err := Enqueue(s, context.Background(), d, "one", Unique("key", time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Del(context.Background(), s.store.keys.job(first.ID)).Err(); err != nil {
		t.Fatal(err)
	}
	second, err := Enqueue(s, context.Background(), d, "two", Unique("key", time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("orphaned unique key was not replaced")
	}
}

func TestCronManagement(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := Define[string]("cron.manage")
	schedule, err := ScheduleCron(s, context.Background(), d, "payload", "@every 1h", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PauseSchedule(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := s.GetSchedule(context.Background(), schedule.ID)
	if err != nil || paused.State != ScheduleStatePaused {
		t.Fatalf("paused=%+v err=%v", paused, err)
	}
	if err := s.ResumeSchedule(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(context.Background(), schedule.ID, "@every 2h", time.UTC); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListSchedules(context.Background(), ScheduleQuery{Limit: 10})
	if err != nil || len(items) != 1 || items[0].Expression != "@every 2h" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := s.DeleteSchedule(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSchedule(context.Background(), schedule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
}

func TestInvalidAndOrphanedCronSchedulesAreQuarantined(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := ScheduleCron(s, context.Background(), Define[string]("cron.invalid"), "payload", "@every 1h", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Second).UnixMilli()
	if err := client.HSet(context.Background(), s.store.keys.schedule(schedule.ID), "cron", "invalid", "next_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(context.Background(), s.store.keys.schedules(), redis.Z{Score: float64(due), Member: schedule.ID}, redis.Z{Score: float64(due), Member: "orphan"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatchSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := client.HGet(context.Background(), s.store.keys.schedule(schedule.ID), "state").Result()
	if err != nil || state != "invalid" {
		t.Fatalf("invalid schedule state=%q err=%v", state, err)
	}
	for _, id := range []string{schedule.ID, "orphan"} {
		if _, err := client.ZScore(context.Background(), s.store.keys.schedules(), id).Result(); !errors.Is(err, redis.Nil) {
			t.Fatalf("schedule %q was not removed: %v", id, err)
		}
	}
}

func TestRunningJobCancellation(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, DispatchInterval: 5 * time.Millisecond, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("cancel.running", WithTimeout(10*time.Second), WithRetry(NoRetry{}))
	started := make(chan struct{})
	if err := Handle(s, d, func(ctx context.Context, _ string) error { close(started); <-ctx.Done(); return ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}
	if err := s.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, func() bool {
		current, getErr := s.Get(context.Background(), job.ID)
		return getErr == nil && current.State == StateCancelled
	})
}

func TestSeparateDispatcherAndWorkerRoles(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	dispatcher, err := New(client, Config{Namespace: namespace, Role: RoleDispatcher, DispatchInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(client, Config{Namespace: namespace, Role: RoleWorker, Concurrency: 1, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("roles.job", WithRetry(NoRetry{}))
	handled := make(chan struct{}, 1)
	if err := Handle(worker, d, func(context.Context, string) error { handled <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- dispatcher.Run(ctx) }()
	go func() { done <- worker.Run(ctx) }()
	job, err := Enqueue(dispatcher, context.Background(), d, "payload", Delay(30*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("separate worker did not execute job")
	}
	eventually(t, time.Second, func() bool {
		current, getErr := worker.Get(context.Background(), job.ID)
		return getErr == nil && current.State == StateSucceeded
	})
	cancel()
	<-done
	<-done
}

func TestImmediateJobDoesNotRequireDispatcher(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Role: RoleWorker, Concurrency: 1, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("immediate.fast", WithRetry(NoRetry{}))
	handled := make(chan struct{}, 1)
	if err := Handle(s, d, func(context.Context, string) error { handled <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	if job.State != StateReady {
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
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.Time(context.Background()).Result()
	if err != nil {
		t.Fatal(err)
	}
	job, err := Enqueue(s, context.Background(), Define[string]("delay.clock"), "payload", Delay(time.Minute))
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

func TestStatsSeparateReadyAndPending(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.ensureGroups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := Enqueue(s, context.Background(), Define[string]("stats.job"), "payload"); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(context.Background())
	if err != nil || stats.Ready != 1 || stats.Pending != 0 {
		t.Fatalf("before receive: stats=%+v err=%v", stats, err)
	}
	if _, err := s.store.receive(context.Background(), "stats-consumer", time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	stats, err = s.Stats(context.Background())
	if err != nil || stats.Ready != 0 || stats.Pending != 1 {
		t.Fatalf("after receive: stats=%+v err=%v", stats, err)
	}
}

func TestRemoteRunningJobCancellation(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	worker, err := New(client, Config{Namespace: namespace, Role: RoleWorker, Concurrency: 1, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second, LeaseRenewInterval: time.Second, CancellationCheckInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	producer, err := New(client, Config{Namespace: namespace, Role: RoleDispatcher})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("cancel.remote", WithTimeout(10*time.Second), WithRetry(NoRetry{}))
	started := make(chan struct{})
	if err := Handle(worker, d, func(ctx context.Context, _ string) error {
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
	job, err := Enqueue(producer, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}
	if err := producer.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		current, getErr := producer.Get(context.Background(), job.ID)
		return getErr == nil && current.State == StateCancelled
	})
}

func TestJanitorCleansIndexesInBoundedBatches(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, MaintenanceBatch: 2, MaintenanceDrainLimit: 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now, err := client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.store.keys.dead(),
		redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "expired-1"},
		redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "expired-2"},
		redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: "retained"},
	).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.store.keys.scheduled(), redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: "orphan-job"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.store.keys.schedules(), redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: "orphan-cron"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.store.keys.scheduleCatalog(), redis.Z{Score: float64(now.UnixMilli()), Member: "orphan-cron"}).Err(); err != nil {
		t.Fatal(err)
	}

	var scheduledCursor, cronCursor uint64
	for range 10 {
		scheduledCursor, cronCursor, err = s.runMaintenance(ctx, scheduledCursor, cronCursor)
		if err != nil {
			t.Fatal(err)
		}
		if scheduledCursor == 0 && cronCursor == 0 {
			break
		}
	}
	if got := client.ZCard(ctx, s.store.keys.dead()).Val(); got != 1 {
		t.Fatalf("dead index size=%d", got)
	}
	if got := client.ZCard(ctx, s.store.keys.scheduled()).Val(); got != 0 {
		t.Fatalf("scheduled index size=%d", got)
	}
	if got := client.ZCard(ctx, s.store.keys.schedules()).Val(); got != 0 {
		t.Fatalf("cron index size=%d", got)
	}
}

func TestJanitorDeletesOnlyIdleConsumersWithoutPendingMessages(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.store.ensureGroups(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.XGroupCreateConsumer(ctx, s.store.keys.ready(), s.store.keys.group(), "empty").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := Enqueue(s, ctx, Define[string]("consumer.cleanup"), "payload"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.receive(ctx, "pending", time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := s.store.cleanupStaleConsumers(ctx, time.Millisecond, 10); err != nil {
		t.Fatal(err)
	}
	consumers, err := client.XInfoConsumers(ctx, s.store.keys.ready(), s.store.keys.group()).Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(consumers) != 1 || consumers[0].Name != "pending" || consumers[0].Pending != 1 {
		t.Fatalf("consumers=%+v", consumers)
	}
}

func TestGracefulShutdownKeepsActiveJobLeasedUntilCompletion(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second, LeaseRenewInterval: 100 * time.Millisecond, CancellationCheckInterval: 50 * time.Millisecond, ShutdownTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("shutdown.complete", WithTimeout(5*time.Second), WithRetry(NoRetry{}))
	started, release := make(chan struct{}), make(chan struct{})
	if err := Handle(s, d, func(context.Context, string) error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := Enqueue(s, context.Background(), d, "payload")
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
	current, err := s.Get(context.Background(), job.ID)
	if err != nil || current.State != StateSucceeded {
		t.Fatalf("job=%+v err=%v", current, err)
	}
}

func TestShutdownTimeoutLeavesJobRecoverable(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Concurrency: 1, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second, LeaseRenewInterval: 100 * time.Millisecond, CancellationCheckInterval: 50 * time.Millisecond, ShutdownTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("shutdown.timeout", WithTimeout(5*time.Second), WithRetry(NoRetry{}))
	started, release := make(chan struct{}), make(chan struct{})
	if err := Handle(s, d, func(context.Context, string) error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	if err := <-done; !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("shutdown error=%v", err)
	}
	close(release)
	current, err := s.Get(context.Background(), job.ID)
	if err != nil || current.State != StateRunning {
		t.Fatalf("job=%+v err=%v", current, err)
	}
}

func TestCancelledReadyMessageIsDeletedFromStream(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace, Role: RoleWorker, Concurrency: 1, PollTimeout: 20 * time.Millisecond, LeaseDuration: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("cancel.ready")
	job, err := Enqueue(s, context.Background(), d, "payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	eventually(t, time.Second, func() bool { return client.XLen(context.Background(), s.store.keys.ready()).Val() == 0 })
	cancel()
	<-done
}

func TestMaintenanceLeaseHasSingleOwner(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	store := store{client: client, keys: newKeys(namespace)}
	first, err := store.acquireMaintenance(context.Background(), "one", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.acquireMaintenance(context.Background(), "two", time.Second)
	if err != nil || !first || second {
		t.Fatalf("first=%v second=%v err=%v", first, second, err)
	}
}

func TestScheduleCatalogIncludesInactiveStates(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := Define[string]("cron.catalog")
	paused, _ := ScheduleCron(s, context.Background(), d, "one", "@every 1h", time.UTC)
	cancelled, _ := ScheduleCron(s, context.Background(), d, "two", "@every 1h", time.UTC)
	if err := s.PauseSchedule(context.Background(), paused.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelSchedule(context.Background(), cancelled.ID); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListSchedules(context.Background(), ScheduleQuery{Limit: 10})
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	items, err := s.ListSchedules(context.Background(), ScheduleQuery{Limit: 10, State: ScheduleStatePaused})
	if err != nil || len(items) != 1 || items[0].ID != paused.ID {
		t.Fatalf("paused=%+v err=%v", items, err)
	}
}

func TestDeadTypeQueryAppliesOffsetAfterGlobalFiltering(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, typ := range []string{"a", "b", "a", "b", "a"} {
		id := fmt.Sprintf("dead-%d", i)
		if err := client.HSet(ctx, s.store.keys.job(id), "id", id, "type", typ, "state", string(StateDead)).Err(); err != nil {
			t.Fatal(err)
		}
		if err := client.ZAdd(ctx, s.store.keys.dead(), redis.Z{Score: float64(i), Member: id}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := s.QueryDead(ctx, DeadQuery{Offset: 1, Limit: 2, Type: "a"})
	if err != nil || len(jobs) != 2 || jobs[0].ID != "dead-2" || jobs[1].ID != "dead-0" {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func BenchmarkBatchEnqueue(b *testing.B) {
	client := testRedis(b)
	namespace := fmt.Sprintf("scheduler-bench-%d", time.Now().UnixNano())
	b.Cleanup(func() { cleanup(b, client, namespace) })
	s, err := New(client, Config{Namespace: namespace})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	d := Define[int]("bench.job")
	payloads := make([]int, 100)
	for b.Loop() {
		results := BatchEnqueue(s, context.Background(), d, payloads)
		for _, result := range results {
			if result.Err != nil {
				b.Fatal(result.Err)
			}
		}
	}
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
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password})
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
