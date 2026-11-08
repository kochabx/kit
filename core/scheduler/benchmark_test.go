package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

var benchmarkFingerprint string

func BenchmarkTaskFingerprint(b *testing.B) {
	d := mustTask[int]("fingerprint")
	b.ReportAllocs()
	for b.Loop() {
		value := d.Fingerprint()
		benchmarkFingerprint = value
	}
}

func BenchmarkClaimSnapshot(b *testing.B) {
	client := testRedis(b)
	ctx := context.Background()
	namespace := fmt.Sprintf("start-bench-%d", time.Now().UnixNano())
	b.Cleanup(func() { cleanup(b, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	job, err := mustBind(s, mustTask[int]("start")).Enqueue(ctx, 42)
	if err != nil {
		b.Fatal(err)
	}
	// Warm the Lua script, then reset only state outside the measured interval.
	if _, err := s.queues[DefaultQueue].store.ClaimJob(ctx, store.Delivery{JobID: job.ID}, store.ClaimCommand{
		Token:              "token",
		Worker:             "worker",
		Duration:           30 * time.Second,
		ExpiredRetention:   time.Hour,
		CancelledRetention: time.Hour,
	}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		if err := client.HSet(ctx, s.queues[DefaultQueue].store.Keys.Job(job.ID), "state", "ready", "attempt", 0).Err(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := s.queues[DefaultQueue].store.ClaimJob(ctx, store.Delivery{JobID: job.ID}, store.ClaimCommand{
			Token:              "token",
			Worker:             "worker",
			Duration:           30 * time.Second,
			ExpiredRetention:   time.Hour,
			CancelledRetention: time.Hour,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCronDispatch32(b *testing.B) {
	client := testRedis(b)
	ctx := context.Background()
	namespace := fmt.Sprintf("cron-bench-%d", time.Now().UnixNano())
	b.Cleanup(func() { cleanup(b, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	ids := make([]string, 32)
	for i := range ids {
		schedule, err := mustBind(s, mustTask[string]("cron")).Schedule(ctx, "payload", CronSpec{
			Expression: "@every 1h",
			TimeZone:   "UTC",
		})
		if err != nil {
			b.Fatal(err)
		}
		ids[i] = schedule.ID
	}
	now, err := s.queues[DefaultQueue].store.ServerTimeMillis(ctx)
	if err != nil {
		b.Fatal(err)
	}
	due := now - 1000
	reset := func() {
		pipe := client.Pipeline()
		pipe.Del(ctx, s.queues[DefaultQueue].store.Keys.Ready(), s.queues[DefaultQueue].store.Keys.Scheduled())
		for _, id := range ids {
			pipe.HSet(ctx, s.queues[DefaultQueue].store.Keys.Schedule(id), "next_at", due)
			pipe.ZAdd(ctx, s.queues[DefaultQueue].store.Keys.Schedules(), redis.Z{Score: float64(due), Member: id})
		}
		if _, err := pipe.Exec(ctx); err != nil {
			b.Fatal(err)
		}
	}
	reset()
	if err := s.queues[DefaultQueue].dispatchSchedules(ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		reset()
		b.StartTimer()
		if err := s.queues[DefaultQueue].dispatchSchedules(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCronNext(b *testing.B) {
	record := store.ScheduleRecord{Expression: "0 9 * * 1-5", TimeZone: "Asia/Shanghai"}
	from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC).UnixMilli()
	b.Run("Uncached", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := nextCronFrom(record, from); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Cached", func(b *testing.B) {
		cache := newCronCache(1024)
		if _, err := cache.nextRunMillis(record, from); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := cache.nextRunMillis(record, from); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBatchEnqueue(b *testing.B) {
	client := testRedis(b)
	namespace := fmt.Sprintf("scheduler-bench-%d", time.Now().UnixNano())
	b.Cleanup(func() { cleanup(b, client, namespace) })
	s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	d := mustTask[int]("bench.job")
	payloads := make([]int, 100)
	for b.Loop() {
		results := mustBind(s, d).EnqueueBatch(context.Background(), payloads)
		for _, result := range results {
			if result.Err != nil {
				b.Fatal(result.Err)
			}
		}
	}
}
