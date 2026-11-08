package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestQueueRetryDeadAndCancellationRoutes(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-retry-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleCombined, "reports", "mail")
	var attempts atomic.Int64
	d := mustTask[int]("report", WithQueue("reports"), WithRetry(Exponential{MaxAttempts: 2, Initial: time.Millisecond}))
	if err := mustBind(s, d).Handle(func(context.Context, int) error {
		switch attempts.Add(1) {
		case 1:
			return errors.New("transient")
		case 2:
			return Permanent(errors.New("permanent"))
		default:
			return nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	runQueueTestScheduler(t, s)
	job, err := mustBind(s, d).Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		current, err := s.Jobs().Get(ctx, job.ID)
		return err == nil && current.State == JobDead
	})
	dead, err := mustQueue(s, "reports").DeadJobs(ctx, DeadQuery{Count: 10})
	if err != nil || len(dead.Items) != 1 || dead.Items[0].Queue != "reports" {
		t.Fatalf("dead=%+v err=%v", dead, err)
	}
	mail, err := mustQueue(s, "mail").Stats(ctx)
	if err != nil || mail.Dead != 0 || mail.Scheduled != 0 {
		t.Fatalf("retry crossed queues: %+v %v", mail, err)
	}
	if err := s.Jobs().Retry(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		current, err := s.Jobs().Get(ctx, job.ID)
		return err == nil && current.State == JobSucceeded && current.Queue == "reports"
	})
	delayed, err := mustBind(s, d).Enqueue(ctx, 2, Delay(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs().Cancel(ctx, delayed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ZScore(ctx, s.queues["reports"].store.Keys.Scheduled(), delayed.ID).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("cancel left queue index: %v", err)
	}
	// DeleteDead must remove the persisted queue's index as well.
	if err := client.HSet(ctx, s.namespace.Keys.Job("dead-manual"), "id", "dead-manual", "queue", "reports", "state", "dead").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.queues["reports"].store.Keys.Dead(), redis.Z{
		Score:  float64(time.Now().UnixMilli()),
		Member: "dead-manual",
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs().Delete(ctx, "dead-manual"); err != nil {
		t.Fatal(err)
	}
	if count := client.ZCard(ctx, s.queues["reports"].store.Keys.Dead()).Val(); count != 0 {
		t.Fatalf("dead index count=%d", count)
	}
}

func TestCronPreservesQueueAndManagementRoutes(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-cron-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleDispatcher, "cron", "other")
	schedule, err := mustBind(s, mustTask[int]("cron", WithQueue("cron"))).Schedule(ctx, 1, CronSpec{
		Expression: "@every 1h",
		TimeZone:   "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Queue != "cron" {
		t.Fatalf("queue=%q", schedule.Queue)
	}
	q := s.queues["cron"]
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	due := now - 1000
	if err := client.HSet(ctx, s.namespace.Keys.Schedule(schedule.ID), "next_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, q.store.Keys.Schedules(), redis.Z{
		Score:  float64(due),
		Member: schedule.ID,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := q.dispatchSchedules(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := s.Jobs().Get(ctx, schedule.ID+":"+fmt.Sprint(due))
	if err != nil || job.Queue != "cron" || job.State != JobReady {
		t.Fatalf("cron job=%+v err=%v", job, err)
	}
	if stats, err := mustQueue(s, "other").Stats(ctx); err != nil || stats.Ready != 0 || stats.Schedules != 0 {
		t.Fatalf("other queue=%+v %v", stats, err)
	}
	if err := s.Schedules().Pause(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if count := client.ZCard(ctx, q.store.Keys.Schedules()).Val(); count != 0 {
		t.Fatal("paused schedule still active")
	}
	if err := s.Schedules().Resume(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().UpdateCron(ctx, schedule.ID, CronSpec{Expression: "@every 2h", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	schedules, err := mustQueue(s, "cron").Schedules(ctx, ScheduleQuery{Count: 10})
	if err != nil || len(schedules.Items) != 1 || schedules.Items[0].Queue != "cron" {
		t.Fatalf("schedules=%+v err=%v", schedules, err)
	}
	if err := s.Schedules().Cancel(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if count := client.ZCard(ctx, q.store.Keys.TerminalSchedules()).Val(); count != 1 {
		t.Fatal("terminal schedule routed incorrectly")
	}
	if err := s.Schedules().Delete(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if count := client.ZCard(ctx, q.store.Keys.ScheduleCatalog()).Val(); count != 0 {
		t.Fatal("schedule catalog not cleaned")
	}
}

func TestWrongQueueDeliveryCannotAcquireJob(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-wrong-delivery-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleWorker, "a", "b")
	job, err := mustBind(s, mustTask[int]("task", WithQueue("a"))).Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	wrong := s.queues["b"]
	if err := wrong.store.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: wrong.store.Keys.Ready(),
		Values: map[string]any{"job_id": job.ID},
	}).Err(); err != nil {
		t.Fatal(err)
	}
	deliveries, err := wrong.store.ReadDeliveries(ctx, "wrong-reader", time.Millisecond, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive=%v %v", deliveries, err)
	}
	if _, err := wrong.store.ClaimJob(ctx, deliveries[0], store.ClaimCommand{
		Token:              "token",
		Worker:             "worker",
		Duration:           3 * time.Second,
		ExpiredRetention:   time.Hour,
		CancelledRetention: time.Hour,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("wrong queue acquired: %v", err)
	}
	current, err := s.Jobs().Get(ctx, job.ID)
	if err != nil || current.State != JobReady || current.Attempt != 0 {
		t.Fatalf("wrong queue mutated job=%+v %v", current, err)
	}
	if count := client.XLen(ctx, wrong.store.Keys.Ready()).Val(); count != 0 {
		t.Fatal("invalid delivery was not discarded")
	}
}

func TestUniqueKeyRemainsNamespaceScopedAcrossQueues(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-unique-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleDispatcher, "a", "b")
	first, err := mustBind(s, mustTask[int]("task", WithQueue("a"))).Enqueue(ctx, 1, Unique("business", time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := mustBind(s, mustTask[int]("task", WithQueue("b"))).Enqueue(ctx, 1, Unique("business", time.Hour))
	if !errors.Is(err, ErrDuplicate) || duplicate == nil || duplicate.ID != first.ID || duplicate.Queue != "a" {
		t.Fatalf("cross-queue duplicate=%+v err=%v", duplicate, err)
	}
	results := mustBind(s, mustTask[int]("task", WithQueue("b"))).EnqueueBatch(ctx, []int{1}, Unique("business", time.Hour))
	if len(results) != 1 || !errors.Is(results[0].Err, ErrDuplicate) || results[0].Job == nil || results[0].Job.Queue != "a" {
		t.Fatalf("batch duplicate=%+v", results)
	}
}

func TestCronWrongQueueIndexDoesNotMisroute(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-cron-stale-index-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleDispatcher, "a", "b")
	schedule, err := mustBind(s, mustTask[int]("task", WithQueue("a"))).Schedule(ctx, 1, CronSpec{
		Expression: "@every 1h",
		TimeZone:   "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	due := now - 1
	if err := client.HSet(ctx, s.namespace.Keys.Schedule(schedule.ID), "next_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	wrong := s.queues["b"]
	if err := client.ZAdd(ctx, wrong.store.Keys.Schedules(), redis.Z{
		Score:  float64(due),
		Member: schedule.ID,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := wrong.dispatchSchedules(ctx); err != nil {
		t.Fatal(err)
	}
	if count := client.XLen(ctx, wrong.store.Keys.Ready()).Val(); count != 0 {
		t.Fatal("foreign schedule entered wrong stream")
	}
	current, err := s.Schedules().Get(ctx, schedule.ID)
	if err != nil || current.NextAt.UnixMilli() != due {
		t.Fatalf("foreign schedule changed: %+v %v", current, err)
	}
	if count := client.ZCard(ctx, wrong.store.Keys.Schedules()).Val(); count != 0 {
		t.Fatal("foreign schedule index not removed")
	}
}

func TestDelayedWrongQueueIndexDoesNotMisroute(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("queue-delay-stale-index-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s := queueTestScheduler(t, client, namespace, RoleDispatcher, "a", "b")
	job, err := mustBind(s, mustTask[int]("task", WithQueue("a"))).Enqueue(ctx, 1, Delay(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	due := now - 1
	if err := client.HSet(ctx, s.namespace.Keys.Job(job.ID), "run_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := client.ZAdd(ctx, s.queues[name].store.Keys.Scheduled(), redis.Z{
			Score:  float64(due),
			Member: job.ID,
		}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	wrong := s.queues["b"]
	if _, err := wrong.store.DispatchJobs(ctx, 10, time.Hour); err != nil {
		t.Fatal(err)
	}
	current, err := s.Jobs().Get(ctx, job.ID)
	if err != nil || current.State != JobScheduled {
		t.Fatalf("wrong queue mutated job=%+v %v", current, err)
	}
	if count := client.XLen(ctx, wrong.store.Keys.Ready()).Val(); count != 0 {
		t.Fatal("foreign delayed job misrouted")
	}
	if count := client.ZCard(ctx, wrong.store.Keys.Scheduled()).Val(); count != 0 {
		t.Fatal("wrong delayed index not removed")
	}
	correct := s.queues["a"]
	if _, err := correct.store.DispatchJobs(ctx, 10, time.Hour); err != nil {
		t.Fatal(err)
	}
	if count := client.XLen(ctx, correct.store.Keys.Ready()).Val(); count != 1 {
		t.Fatal("original queue could not dispatch job")
	}
}
