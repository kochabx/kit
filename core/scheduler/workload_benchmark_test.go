package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

// BenchmarkTaskRoundTrip measures serialized enqueue/read/execute/commit work.
// It is a reproducible baseline, not a multi-node throughput measurement.
func BenchmarkTaskRoundTrip(b *testing.B) {
	for _, workload := range []struct {
		name   string
		bytes  int
		delay  time.Duration
		queues int
	}{
		{"Short", 64, 0, 1}, {"Long", 64, 5 * time.Millisecond, 1},
		{"LargePayload", 256 << 10, 0, 1}, {"MultiQueue", 64, 0, 8},
	} {
		b.Run(workload.name, func(b *testing.B) {
			client := testRedis(b)
			namespace := fmt.Sprintf("roundtrip-%d", time.Now().UnixNano())
			b.Cleanup(func() { cleanup(b, client, namespace) })
			configs := make([]QueueConfig, workload.queues)
			for i := range configs {
				configs[i] = QueueConfig{Name: fmt.Sprintf("q%d", i), Concurrency: 1}
			}
			s, err := newWorkerRuntime(client, WorkerConfig{Namespace: namespace, Queues: configs})
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			tasks := make([]*testProducer[string], workload.queues)
			for i := range tasks {
				task, err := newTaskClient[string](s, "work", WithQueue(configs[i].Name))
				if err != nil {
					b.Fatal(err)
				}
				if err := task.Handle(func(context.Context, string) error {
					if workload.delay > 0 {
						time.Sleep(workload.delay)
					}
					return nil
				}); err != nil {
					b.Fatal(err)
				}
				tasks[i] = task
				if err := s.queues[configs[i].Name].store.EnsureConsumerGroup(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
			payload := strings.Repeat("x", workload.bytes)
			ctx := context.Background()
			iterations := 0
			b.ReportAllocs()
			b.SetBytes(int64(workload.bytes))
			b.ResetTimer()
			for b.Loop() {
				index := iterations % len(tasks)
				q := s.queues[configs[index].Name]
				if _, err := tasks[index].Enqueue(ctx, payload); err != nil {
					b.Fatal(err)
				}
				deliveries, err := q.store.ReadDeliveries(ctx, "bench", time.Millisecond, 1)
				if err != nil || len(deliveries) != 1 {
					b.Fatalf("receive: %v %v", deliveries, err)
				}
				q.execute(ctx, "bench", deliveries[0])
				iterations++
			}
			b.ReportMetric(float64(iterations)/b.Elapsed().Seconds(), "jobs/s")
		})
	}
}

func BenchmarkCronCacheManyRules(b *testing.B) {
	for _, size := range []int{128, 4096} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			cache := newCronCache(size)
			records := make([]store.ScheduleRecord, size)
			for i := range records {
				records[i] = store.ScheduleRecord{Expression: fmt.Sprintf("@every %ds", i+1), TimeZone: "UTC"}
			}
			from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC).UnixMilli()
			for _, record := range records {
				if _, err := cache.nextRunMillis(record, from); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			index := 0
			for b.Loop() {
				if _, err := cache.nextRunMillis(records[index%size], from); err != nil {
					b.Fatal(err)
				}
				index++
			}
		})
	}
}

func BenchmarkCronDispatchScale(b *testing.B) {
	for _, count := range []int{128, 1024} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			client := testRedis(b)
			namespace := fmt.Sprintf("cron-scale-%d", time.Now().UnixNano())
			b.Cleanup(func() { cleanup(b, client, namespace) })
			s, err := newWorkerRuntime(client, WorkerConfig{
				Namespace: namespace,
				Dispatch: DispatchConfig{
					Batch:          128,
					CronDrainLimit: int64(count),
				},
			})
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			task, err := newTaskClient[int](s, "cron.work")
			if err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			schedules := make([]*Schedule, count)
			for i := range schedules {
				schedule, err := task.Schedule(ctx, i, CronSpec{Expression: "@every 1h"})
				if err != nil {
					b.Fatal(err)
				}
				schedules[i] = schedule
			}
			q := s.queues[DefaultQueue]
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				b.StopTimer()
				pipe := client.Pipeline()
				for _, schedule := range schedules {
					pipe.HSet(ctx, q.store.Keys.Schedule(schedule.ID), "next_at", 1)
					pipe.ZAdd(ctx, q.store.Keys.Schedules(), redis.Z{Score: 1, Member: schedule.ID})
				}
				pipe.Del(ctx, q.store.Keys.Ready())
				if _, err := pipe.Exec(ctx); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := q.dispatchSchedules(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
