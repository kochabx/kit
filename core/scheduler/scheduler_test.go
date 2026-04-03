package scheduler

import (
	"context"
	"errors"
	"fmt"
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

// testRedisClient 获取测试用 Redis 客户端，不可用则 Skip
func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", redisAddr, err)
	}
	return rdb
}

// newTestScheduler 创建测试用调度器，带常用默认值
func newTestScheduler(t *testing.T, rdb *redis.Client, extraOpts ...Option) (*Scheduler, string) {
	t.Helper()
	namespace := "test-" + uuid.NewString()[:8]

	baseOpts := []Option{
		WithRedisClient(rdb),
		WithNamespace(namespace),
		WithWorkerCount(1),
		WithWorkerConcurrency(2),
		WithScanInterval(20 * time.Millisecond),
		WithBatchSize(20),
		WithDeduplication(false, 0),
		WithDLQ(true, 100),
		WithMetrics(false),
		WithHealth(false),
		func(o *Options) {
			o.Worker.LeaseTTL = 2 * time.Second
			o.Worker.RenewInterval = 200 * time.Millisecond
			o.Worker.ShutdownGracePeriod = 2 * time.Second
			o.LockTimeout = 2 * time.Second
			o.Retry.BaseDelay = 50 * time.Millisecond
			o.Retry.MaxDelay = 500 * time.Millisecond
			o.Retry.Multiplier = 2.0
			o.Retry.Jitter = false
		},
	}
	baseOpts = append(baseOpts, extraOpts...)

	s, err := New(baseOpts...)
	if err != nil {
		t.Fatalf("New scheduler: %v", err)
	}

	t.Cleanup(func() {
		cleanupNamespace(t, rdb, namespace)
	})

	if q, ok := s.queue.(*Queue); ok {
		_ = q.Clear(context.Background())
	}

	return s, namespace
}

type testPayloadMsg struct {
	Value string `json:"value"`
}

// ─── Retry + DLQ ───────────────────────────────────────────

func TestScheduler_RetryAndDLQ(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb)

	var attempts atomic.Int64
	errFail := errors.New("always fail")

	if err := SchedulerRegister[testPayloadMsg](s, "retry.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		attempts.Add(1)
		return errFail
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutCtx)
	})

	// maxRetry=2 → handler 被调用 2 次后进入 DLQ
	taskID, err := Submit[testPayloadMsg](s, ctx, "retry.test", testPayloadMsg{Value: "fail"},
		WithTaskMaxRetry(2),
		WithPriority(PriorityNormal),
		WithTaskTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 等待任务进入 DLQ（最多等 10s）
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for task to enter DLQ, attempts=%d, taskID=%s", attempts.Load(), taskID)
		default:
		}

		count, err := s.dlq.Count(ctx)
		if err == nil && count > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 验证 handler 被调用了 maxRetry 次
	got := attempts.Load()
	if got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}

	// 验证任务元数据已删除（进入 DLQ 后删除）
	_, err = s.GetTaskInfo(ctx, taskID)
	if err != ErrTaskNotFound {
		t.Fatalf("expected task metadata deleted after DLQ, err=%v", err)
	}
}

// ─── Delayed Task ──────────────────────────────────────────

func TestScheduler_DelayedTask(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb)

	executed := make(chan time.Time, 1)
	if err := SchedulerRegister[testPayloadMsg](s, "delay.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		executed <- time.Now()
		return nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutCtx)
	})

	// 延迟队列使用 Unix 秒级精度 (ScheduleAt.Unix())，因此实际执行时间
	// 可能比请求的 delay 短 ~1 秒。使用 2 秒延迟 + 1 秒最小期望。
	submitTime := time.Now()
	delay := 2 * time.Second
	_, err := Submit[testPayloadMsg](s, ctx, "delay.test", testPayloadMsg{Value: "delayed"},
		WithDelay(delay),
		WithPriority(PriorityNormal),
		WithTaskTimeout(5*time.Second),
		WithTaskMaxRetry(0),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case execTime := <-executed:
		elapsed := execTime.Sub(submitTime)
		if elapsed < 1*time.Second {
			t.Fatalf("task executed too early: elapsed=%v, expected >= 1s", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for delayed task")
	}
}

// ─── Deduplication ─────────────────────────────────────────

func TestScheduler_Deduplication(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb, WithDeduplication(true, 5*time.Second))

	var calls atomic.Int64
	if err := SchedulerRegister[testPayloadMsg](s, "dedup.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		calls.Add(1)
		return nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutCtx)
	})

	// 提交两次相同 dedup key
	_, err := Submit[testPayloadMsg](s, ctx, "dedup.test", testPayloadMsg{Value: "a"},
		WithTaskDeduplication("same-key", 5*time.Second),
		WithPriority(PriorityNormal),
		WithTaskTimeout(2*time.Second),
		WithTaskMaxRetry(0),
	)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	_, err = Submit[testPayloadMsg](s, ctx, "dedup.test", testPayloadMsg{Value: "b"},
		WithTaskDeduplication("same-key", 5*time.Second),
		WithPriority(PriorityNormal),
		WithTaskTimeout(2*time.Second),
		WithTaskMaxRetry(0),
	)
	if !errors.Is(err, ErrTaskDuplicate) {
		t.Fatalf("expected ErrTaskDuplicate, got %v", err)
	}

	// 等待第一个任务执行完成
	time.Sleep(1 * time.Second)

	if calls.Load() != 1 {
		t.Fatalf("expected 1 handler call, got %d", calls.Load())
	}
}

// ─── Batch Submit ──────────────────────────────────────────

func TestScheduler_BatchSubmit(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb)

	var received sync.Map
	var count atomic.Int64

	if err := SchedulerRegister[testPayloadMsg](s, "batch.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		received.Store(p.Value, true)
		count.Add(1)
		return nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutCtx)
	})

	payloads := []testPayloadMsg{
		{Value: "batch-1"},
		{Value: "batch-2"},
		{Value: "batch-3"},
	}

	taskIDs, err := BatchSubmit[testPayloadMsg](s, ctx, "batch.test", payloads,
		WithPriority(PriorityNormal),
		WithTaskTimeout(2*time.Second),
		WithTaskMaxRetry(0),
	)
	if err != nil {
		t.Fatalf("BatchSubmit: %v", err)
	}
	if len(taskIDs) != 3 {
		t.Fatalf("expected 3 task IDs, got %d", len(taskIDs))
	}

	deadline := time.After(5 * time.Second)
	for count.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for all batch tasks, count=%d", count.Load())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	for _, p := range payloads {
		if _, ok := received.Load(p.Value); !ok {
			t.Fatalf("missing payload: %s", p.Value)
		}
	}
}

// ─── Task Timeout ──────────────────────────────────────────

func TestScheduler_TaskTimeout(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb)

	handlerStarted := make(chan struct{})
	handlerCtxDone := make(chan struct{})

	if err := SchedulerRegister[testPayloadMsg](s, "timeout.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		close(handlerStarted)
		<-ctx.Done()
		close(handlerCtxDone)
		return ctx.Err()
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutCtx)
	})

	_, err := Submit[testPayloadMsg](s, ctx, "timeout.test", testPayloadMsg{Value: "slow"},
		WithTaskTimeout(200*time.Millisecond),
		WithPriority(PriorityNormal),
		WithTaskMaxRetry(0),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case <-handlerStarted:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for handler to start")
	}

	select {
	case <-handlerCtxDone:
		// context was cancelled due to timeout — correct
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for task context to be cancelled")
	}
}

// ─── Cancel Task ───────────────────────────────────────────

func TestScheduler_CancelTask(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb)

	var calls atomic.Int64
	if err := SchedulerRegister[testPayloadMsg](s, "cancel.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		calls.Add(1)
		return nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutCtx)
	})

	// 延迟 10 秒执行，我们有时间取消
	taskID, err := Submit[testPayloadMsg](s, ctx, "cancel.test", testPayloadMsg{Value: "cancel-me"},
		WithDelay(10*time.Second),
		WithPriority(PriorityNormal),
		WithTaskTimeout(2*time.Second),
		WithTaskMaxRetry(0),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 取消
	if err := s.CancelTask(ctx, taskID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	// 验证状态
	info, err := s.GetTaskInfo(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTaskInfo: %v", err)
	}
	if info.Status != StatusCancelled {
		t.Fatalf("expected status %s, got %s", StatusCancelled, info.Status)
	}

	// 等一段时间确认 handler 没被调用
	time.Sleep(500 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("expected 0 handler calls, got %d", calls.Load())
	}
}

// ─── Priority Order ────────────────────────────────────────

func TestScheduler_PriorityOrder(t *testing.T) {
	rdb := testRedisClient(t)
	// 不启动 worker，手工验证 pop 顺序
	s, _ := newTestScheduler(t, rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	q, ok := s.queue.(*Queue)
	if !ok {
		t.Skip("queue is not *Queue")
	}
	q.SetConsumer("test-consumer")

	// 先提交 low，再 normal，再 high
	for _, p := range []struct {
		id       string
		priority Priority
	}{
		{"low-1", PriorityLow},
		{"normal-1", PriorityNormal},
		{"high-1", PriorityHigh},
	} {
		if err := q.AddReady(ctx, p.id, p.priority); err != nil {
			t.Fatalf("AddReady(%s): %v", p.id, err)
		}
	}

	// PopReady 应该按 high → normal → low 顺序
	expected := []string{"high-1", "normal-1", "low-1"}
	for _, exp := range expected {
		taskID, _, _, err := q.PopReady(ctx, 1)
		if err != nil {
			t.Fatalf("PopReady: %v", err)
		}
		if taskID != exp {
			t.Fatalf("expected %s, got %s", exp, taskID)
		}
	}
}

// ─── Concurrent Dedup (SetNX atomicity) ────────────────────

func TestScheduler_ConcurrentDedup(t *testing.T) {
	rdb := testRedisClient(t)
	s, _ := newTestScheduler(t, rdb, WithDeduplication(true, 5*time.Second))

	if err := SchedulerRegister[testPayloadMsg](s, "cdedup.test", HandlerFunc[testPayloadMsg](func(ctx context.Context, p testPayloadMsg) error {
		return nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()

	// 并发提交 20 个相同 dedup key 的任务
	const n = 20
	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		dupes     atomic.Int64
	)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Submit[testPayloadMsg](s, ctx, "cdedup.test",
				testPayloadMsg{Value: fmt.Sprintf("v%d", i)},
				WithTaskDeduplication("concurrent-key", 5*time.Second),
				WithPriority(PriorityNormal),
				WithTaskTimeout(2*time.Second),
				WithTaskMaxRetry(0),
			)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrTaskDuplicate) {
				dupes.Add(1)
			}
		}(i)
	}
	wg.Wait()

	// 正好 1 个成功，其余全部重复
	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 success, got %d (dupes=%d)", successes.Load(), dupes.Load())
	}
	if successes.Load()+dupes.Load() != int64(n) {
		t.Fatalf("expected %d total, got success=%d dupes=%d", n, successes.Load(), dupes.Load())
	}
}
