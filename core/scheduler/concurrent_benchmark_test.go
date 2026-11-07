package scheduler

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkConcurrentRoundTrip exercises the public Client/Worker APIs and waits
// for each handler. Percentiles measure enqueue-to-handler completion, excluding
// the final state commit. It is a single-node baseline, not a fleet capacity claim.
func BenchmarkConcurrentRoundTrip(b *testing.B) {
	r := testRedis(b)
	ns := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())
	b.Cleanup(func() { cleanup(b, r, ns) })
	c, err := NewClient(r, ClientConfig{Namespace: ns})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	w, err := NewWorker(r, WorkerConfig{Namespace: ns, Queues: []QueueConfig{{Name: DefaultQueue, Concurrency: 16}}, Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond}})
	if err != nil {
		b.Fatal(err)
	}
	task, err := NewTask[int64]("benchmark")
	if err != nil {
		b.Fatal(err)
	}
	var waiters sync.Map
	if err := w.Register(task, func(_ context.Context, id int64) error {
		if ch, ok := waiters.LoadAndDelete(id); ok {
			close(ch.(chan struct{}))
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			b.Error(err)
		}
	}()
	var seq atomic.Int64
	var mu sync.Mutex
	samples := make([]int64, 0)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		local := make([]int64, 0)
		for pb.Next() {
			id := seq.Add(1)
			ch := make(chan struct{})
			waiters.Store(id, ch)
			start := time.Now()
			if _, err := c.Enqueue(ctx, task, id); err != nil {
				b.Error(err)
				return
			}
			timer := time.NewTimer(10 * time.Second)
			select {
			case <-ch:
			case <-timer.C:
				b.Error("handler timed out")
				return
			}
			timer.Stop()
			local = append(local, time.Since(start).Nanoseconds())
		}
		mu.Lock()
		samples = append(samples, local...)
		mu.Unlock()
	})
	b.StopTimer()
	b.ReportMetric(float64(seq.Load())/b.Elapsed().Seconds(), "jobs/s")
	slices.Sort(samples)
	if len(samples) > 0 {
		b.ReportMetric(float64(samples[(len(samples)-1)*95/100])/1e6, "p95-ms")
		b.ReportMetric(float64(samples[(len(samples)-1)*99/100])/1e6, "p99-ms")
	}
}
