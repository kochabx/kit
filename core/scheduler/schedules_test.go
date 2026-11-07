package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCronCreatesIndependentJob(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("cron.job")
	schedule, err := mustBind(s, d).Schedule(context.Background(), "payload", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	// Force one occurrence due; keep the next one far enough away that the
	// duplicate assertion does not race the wall-clock tick.
	due := time.Now().Add(-time.Second).UnixMilli()
	if err := client.HSet(context.Background(), s.queues[DefaultQueue].store.Keys.Schedule(schedule.ID), "next_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(context.Background(), s.queues[DefaultQueue].store.Keys.Schedules(), redis.Z{Score: float64(due), Member: schedule.ID}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.queues[DefaultQueue].dispatchSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := client.XRange(context.Background(), s.queues[DefaultQueue].store.Keys.Ready(), "-", "+").Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("cron instances = %v, %v", messages, err)
	}
	if messages[0].Values["job_id"] != schedule.ID+":"+fmt.Sprint(due) {
		t.Fatalf("instance id = %q", messages[0].Values["job_id"])
	}
	if err := s.queues[DefaultQueue].dispatchSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count := client.XLen(context.Background(), s.queues[DefaultQueue].store.Keys.Ready()).Val(); count != 1 {
		t.Fatalf("duplicate cron occurrence: %d", count)
	}
}

func TestCronManagement(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := mustTask[string]("cron.manage")
	schedule, err := mustBind(s, d).Schedule(context.Background(), "payload", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().Pause(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := s.Schedules().Get(context.Background(), schedule.ID)
	if err != nil || paused.State != SchedulePaused {
		t.Fatalf("paused=%+v err=%v", paused, err)
	}
	if err := s.Schedules().Resume(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().UpdateCron(context.Background(), schedule.ID, CronSpec{Expression: "@every 2h", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	items, err := mustQueue(s, DefaultQueue).Schedules(context.Background(), ScheduleQuery{Count: 10})
	if err != nil || len(items.Items) != 1 || items.Items[0].Expression != "@every 2h" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := s.Schedules().Delete(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Schedules().Get(context.Background(), schedule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
}

func TestInvalidAndOrphanedSchedulesAreQuarantined(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := mustBind(s, mustTask[string]("cron.invalid")).Schedule(context.Background(), "payload", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Second).UnixMilli()
	if err := client.HSet(context.Background(), s.queues[DefaultQueue].store.Keys.Schedule(schedule.ID), "cron", "invalid", "next_at", due).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(context.Background(), s.queues[DefaultQueue].store.Keys.Schedules(), redis.Z{Score: float64(due), Member: schedule.ID}, redis.Z{Score: float64(due), Member: "orphan"}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.queues[DefaultQueue].dispatchSchedules(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := client.HGet(context.Background(), s.queues[DefaultQueue].store.Keys.Schedule(schedule.ID), "state").Result()
	if err != nil || state != "invalid" {
		t.Fatalf("invalid schedule state=%q err=%v", state, err)
	}
	for _, id := range []string{schedule.ID, "orphan"} {
		if _, err := client.ZScore(context.Background(), s.queues[DefaultQueue].store.Keys.Schedules(), id).Result(); !errors.Is(err, redis.Nil) {
			t.Fatalf("schedule %q was not removed: %v", id, err)
		}
	}
}

func TestCancelledScheduleExpiresFromCatalog(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Retention: RetentionConfig{Schedule: 5 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := mustBind(s, mustTask[string]("cron.expires")).Schedule(context.Background(), "payload", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().Cancel(context.Background(), schedule.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := s.queues[DefaultQueue].store.PurgeExpiredSchedules(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Schedules().Get(context.Background(), schedule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get schedule err=%v", err)
	}
}

func TestScheduleCatalogIncludesInactiveStates(t *testing.T) {
	client := testRedis(t)
	namespace := fmt.Sprintf("scheduler-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	d := mustTask[string]("cron.catalog")
	paused, _ := mustBind(s, d).Schedule(context.Background(), "one", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	cancelled, _ := mustBind(s, d).Schedule(context.Background(), "two", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	if err := s.Schedules().Pause(context.Background(), paused.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().Cancel(context.Background(), cancelled.ID); err != nil {
		t.Fatal(err)
	}
	all, err := mustQueue(s, DefaultQueue).Schedules(context.Background(), ScheduleQuery{Count: 10})
	if err != nil || len(all.Items) != 2 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	items, err := mustQueue(s, DefaultQueue).Schedules(context.Background(), ScheduleQuery{Count: 10, State: SchedulePaused})
	if err != nil || len(items.Items) != 1 || items.Items[0].ID != paused.ID {
		t.Fatalf("paused=%+v err=%v", items, err)
	}
}

func TestCronBatchConcurrentDispatch(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("cron-batch-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Dispatch: DispatchConfig{Batch: 100}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now, err := s.queues[DefaultQueue].store.ServerTimeMillis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	due := now - 1000
	const count = 32
	for range count {
		schedule, err := mustBind(s, mustTask[string]("batch")).Schedule(ctx, strings.Repeat("x", 4096), CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
		if err != nil {
			t.Fatal(err)
		}
		pipe := client.Pipeline()
		pipe.HSet(ctx, s.queues[DefaultQueue].store.Keys.Schedule(schedule.ID), "next_at", due)
		pipe.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Schedules(), redis.Z{Score: float64(due), Member: schedule.ID})
		if _, err := pipe.Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- s.queues[DefaultQueue].dispatchSchedules(ctx) }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	messages, err := client.XRange(ctx, s.queues[DefaultQueue].store.Keys.Ready(), "-", "+").Result()
	if err != nil || len(messages) != count {
		t.Fatalf("ready count=%d err=%v", len(messages), err)
	}
	seen := map[string]bool{}
	for _, message := range messages {
		id := message.Values["job_id"].(string)
		if seen[id] {
			t.Fatalf("duplicate occurrence %s", id)
		}
		seen[id] = true
		job, err := s.Jobs().Get(ctx, id)
		if err != nil || job.State != JobReady || len(job.Payload) == 0 {
			t.Fatalf("cron job=%+v err=%v", job, err)
		}
	}
	if count := client.ZCard(ctx, s.queues[DefaultQueue].store.Keys.Scheduled()).Val(); count != 0 {
		t.Fatalf("cron delayed again: %d", count)
	}
}

func TestCronOrphansDoNotConsumeDrainProgress(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("cron-orphans-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Dispatch: DispatchConfig{Batch: 2, CronDrainLimit: 4}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	schedule, err := mustBind(s, mustTask[string]("valid")).Schedule(ctx, "payload", CronSpec{Expression: "@every 1h", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	now, err := s.queues[DefaultQueue].store.ServerTimeMillis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, s.queues[DefaultQueue].store.Keys.Schedule(schedule.ID), "next_at", now-1).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Schedules(), redis.Z{Score: float64(now - 3), Member: "orphan1"}, redis.Z{Score: float64(now - 2), Member: "orphan2"}, redis.Z{Score: float64(now - 1), Member: schedule.ID}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.queues[DefaultQueue].dispatchSchedules(ctx); err != nil {
		t.Fatal(err)
	}
	if count := client.XLen(ctx, s.queues[DefaultQueue].store.Keys.Ready()).Val(); count != 1 {
		t.Fatalf("valid schedule starved behind orphans: %d", count)
	}
}

func TestCronSpecRejectsAmbiguousZonesAndMissingOccurrences(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("cron-spec-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	task := mustBind(s, mustTask[int]("cron-spec"))
	for _, spec := range []CronSpec{
		{Expression: "0 0 30 2 *", TimeZone: "UTC"},
		{Expression: "@every 1h", TimeZone: "Local"},
		{Expression: "CRON_TZ=Asia/Shanghai 0 9 * * *", TimeZone: "UTC"},
		{Expression: "@every 1h", TimeZone: "invalid/zone"},
	} {
		if _, err := task.Schedule(ctx, 1, spec); err == nil {
			t.Fatalf("invalid spec accepted: %+v", spec)
		}
	}
	records, err := mustQueue(s, DefaultQueue).Schedules(ctx, ScheduleQuery{Count: 10})
	if err != nil || len(records.Items) != 0 {
		t.Fatalf("invalid schedule persisted: %+v %v", records, err)
	}
	schedule, err := task.Schedule(ctx, 1, CronSpec{Expression: "@every 1h"})
	if err != nil || schedule.TimeZone != "UTC" {
		t.Fatalf("default time zone=%+v err=%v", schedule, err)
	}
	if err := s.Schedules().UpdateCron(ctx, schedule.ID, CronSpec{Expression: "0 0 30 2 *"}); err == nil {
		t.Fatal("updated recurrence to impossible expression")
	}
	current, err := s.Schedules().Get(ctx, schedule.ID)
	if err != nil || current.Expression != "@every 1h" {
		t.Fatal("failed update mutated recurrence")
	}
}

func TestScheduleEditsPreservePauseAndCancellationIsTerminal(t *testing.T) {
	client := testRedis(t)
	ctx := context.Background()
	namespace := fmt.Sprintf("schedule-states-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	schedule, err := mustBind(s, mustTask[int]("state-machine")).Schedule(ctx, 1, CronSpec{Expression: "@every 1h"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().Pause(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().UpdateCron(ctx, schedule.ID, CronSpec{Expression: "@every 2h"}); err != nil {
		t.Fatal(err)
	}
	paused, err := s.Schedules().Get(ctx, schedule.ID)
	if err != nil || paused.State != SchedulePaused {
		t.Fatalf("edit resumed schedule=%+v %v", paused, err)
	}
	if err := s.Schedules().Cancel(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedules().Resume(ctx, schedule.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cancelled schedule resumed: %v", err)
	}
	if err := s.Schedules().UpdateCron(ctx, schedule.ID, CronSpec{Expression: "@every 3h"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cancelled schedule edited: %v", err)
	}
}
