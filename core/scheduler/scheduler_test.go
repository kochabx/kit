package scheduler

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestScheduler_ProduceConsumeFlow(t *testing.T) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")
	if redisPassword == "" {
		redisPassword = "12345678"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
	})
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pingCancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", redisAddr, err)
	}

	namespace := "scheduler-e2e-" + uuid.NewString()[:8]
	s, err := New(
		WithRedisClient(rdb),
		WithNamespace(namespace),
		WithWorkerCount(1),
		WithWorkerConcurrency(1),
		WithScanInterval(20*time.Millisecond),
		WithBatchSize(20),
		WithDeduplication(false, 0),
		WithDLQ(false, 0),
		WithMetrics(false),
		WithHealth(false),
		func(o *Options) {
			o.Worker.LeaseTTL = 2 * time.Second
			o.Worker.RenewInterval = 200 * time.Millisecond
			o.Worker.ShutdownGracePeriod = 2 * time.Second
			o.LockTimeout = 2 * time.Second
		},
	)
	if err != nil {
		t.Fatalf("New scheduler: %v", err)
	}

	t.Cleanup(func() {
		cleanupNamespace(t, rdb, namespace)
	})

	// 确保队列干净（即使 namespace 冲突也尽量自愈）
	if q, ok := s.queue.(*Queue); ok {
		_ = q.Clear(context.Background())
	}

	type testPayload struct {
		N int `json:"n"`
	}

	started := make(chan struct{})
	allowFinish := make(chan struct{})
	done := make(chan testPayload, 1)
	var calls atomic.Int64
	var startOnce sync.Once
	if err := SchedulerRegister[testPayload](s, "e2e.test", HandlerFunc[testPayload](func(ctx context.Context, p testPayload) error {
		t.Logf("handler executing, payload=%+v", p)
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		select {
		case <-allowFinish:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case done <- p:
		default:
		}
		return nil
	})); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	runCtx := t.Context()
	if err := s.Start(runCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
	})

	taskID, err := Submit[testPayload](
		s,
		context.Background(),
		"e2e.test",
		testPayload{N: 42},
		WithPriority(PriorityNormal),
		WithTaskTimeout(2*time.Second),
		WithTaskMaxRetry(0),
		WithScheduleAt(time.Now()),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 等待 Worker 真正开始执行（进入 handler），并在放行前校验任务处于 running。
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting worker to start executing, task_id=%s", taskID)
	}

	runningCtx, runningCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer runningCancel()
	for {
		info, err := s.GetTaskInfo(runningCtx, taskID)
		if err == nil {
			if info.Status != StatusRunning {
				t.Fatalf("expected status running, got %s", info.Status)
			}
			if info.WorkerID == "" {
				t.Fatalf("expected worker_id to be set")
			}
			if info.StartTime == nil {
				t.Fatalf("expected start_time to be set")
			}
			break
		}
		if runningCtx.Err() != nil {
			t.Fatalf("expected task to be running, last err=%v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 任务执行中，消息应处于 pending（尚未 ACK）。
	if q, ok := s.queue.(*Queue); ok {
		pendingCtx, pendingCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer pendingCancel()
		for {
			pending, err := q.GetPendingCount(pendingCtx, PriorityNormal)
			if err == nil && pending > 0 {
				break
			}
			if pendingCtx.Err() != nil {
				t.Fatalf("expected pending message while running, last err=%v", err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	close(allowFinish)

	// 等待消费完成
	select {
	case got := <-done:
		if got.N != 42 {
			t.Fatalf("unexpected payload: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting task to be consumed, task_id=%s", taskID)
	}

	// 稍微等待一个 tick，确保没有重复执行（成功任务按设计应只执行一次）。
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("expected handler called once, got %d", calls.Load())
	}

	// 成功后任务元数据会被删除
	delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer delCancel()
	for {
		_, err := s.GetTaskInfo(delCtx, taskID)
		if err == ErrTaskNotFound {
			break
		}
		if delCtx.Err() != nil {
			t.Fatalf("expected task metadata to be deleted, last err=%v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 队列侧断言：延迟队列应为空，pending（未 ACK）应为 0
	if q, ok := s.queue.(*Queue); ok {
		ctx := context.Background()
		delayed, err := q.GetDelayedCount(ctx)
		if err != nil {
			t.Fatalf("GetDelayedCount: %v", err)
		}
		if delayed != 0 {
			t.Fatalf("expected delayed queue empty, delayed=%d", delayed)
		}

		pending, err := q.GetPendingCount(ctx, PriorityNormal)
		if err != nil {
			t.Fatalf("GetPendingCount: %v", err)
		}
		if pending != 0 {
			t.Fatalf("expected no pending messages, pending=%d", pending)
		}
	}
}

func cleanupNamespace(t *testing.T, rdb *redis.Client, namespace string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var cursor uint64
	pattern := namespace + ":*"
	for {
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 1000).Result()
		if err != nil {
			t.Logf("cleanup scan failed: %v", err)
			return
		}
		if len(keys) > 0 {
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				t.Logf("cleanup del failed: %v", err)
				return
			}
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}
