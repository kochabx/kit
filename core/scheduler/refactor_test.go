package scheduler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
)

func TestPublicClientWorkerAndVersions(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("public-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	c, err := NewClient(r, ClientConfig{
		Namespace: ns,
		Queues:    []string{"mail"},
		Retention: RetentionConfig{Succeeded: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	w, err := NewWorker(
		r,
		WorkerConfig{
			Namespace: ns,
			Queues: []QueueConfig{{
				Name:        "mail",
				Concurrency: 2,
			}},
			Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond},
			Retention: RetentionConfig{Succeeded: time.Minute},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	v1 := mustTask[string]("email", WithQueue("mail"), WithVersion("1"), WithTimeout(time.Second))
	v2 := mustTask[string]("email", WithQueue("mail"), WithVersion("2"))
	handled := make(chan string, 2)
	if err := w.Register(v1, func(_ context.Context, v string) error { handled <- "1:" + v; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := w.Register(v2, func(_ context.Context, v string) error { handled <- "2:" + v; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	// Worker timeout differs: the persisted producer policy must remain authoritative.
	producer := mustTask[string]("email", WithQueue("mail"), WithTimeout(2*time.Second))
	j1, err := c.Enqueue(ctx, producer, "a")
	if err != nil {
		t.Fatal(err)
	}
	j2, err := c.Enqueue(ctx, v2, "b")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case v := <-handled:
			seen[v] = true
		case <-time.After(3 * time.Second):
			t.Fatal("handler missing")
		}
	}
	if !seen["1:a"] || !seen["2:b"] {
		t.Fatal(seen)
	}
	eventually(t, time.Second, func() bool {
		j, e := c.Jobs().Get(ctx, j1.ID)
		return e == nil && j.State == JobSucceeded && j.Policy.Timeout == 2*time.Second
	})
	if j2.Version != "2" {
		t.Fatalf("version=%q", j2.Version)
	}
	sc, err := c.Schedules().Create(ctx, v1, "cron", CronSpec{Expression: "@every 1h"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Schedules().Pause(ctx, sc.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.Register(v1, func(context.Context, string) error { return nil }); !errors.Is(err, ErrAlreadyRun) {
		t.Fatal(err)
	}
}

func TestSubmissionReceiptAfterJobDeletionAndConflict(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("submission-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	c, err := NewClient(r, ClientConfig{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	task := mustTask[int]("receipt")
	ctx := context.Background()
	j, err := c.Enqueue(ctx, task, 1, WithJobID("request-1"))
	if err != nil {
		t.Fatal(err)
	}
	same, err := c.Enqueue(ctx, task, 1, WithJobID("request-1"))
	if !errors.Is(err, ErrDuplicate) || same == nil || same.ID != j.ID {
		t.Fatalf("%+v %v", same, err)
	}
	if _, err := c.Enqueue(ctx, task, 2, WithJobID("request-1")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	// Simulate terminal entity removal; the separate submission receipt must survive.
	if err := c.Jobs().Cancel(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Jobs().Delete(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	same, err = c.Enqueue(ctx, task, 1, WithJobID("request-1"))
	if same != nil || !errors.Is(err, ErrDuplicate) {
		t.Fatalf("%+v %v", same, err)
	}
	if n := r.XLen(ctx, c.state.queueStores[DefaultQueue].store.Keys.Ready()).Val(); n != 1 {
		t.Fatalf("duplicate delivery: %d", n)
	}
	results := c.EnqueueBatch(ctx, task, []int{1, 2}, WithJobID("batch"))
	if results[0].Err != nil || !errors.Is(results[1].Err, ErrConflict) {
		t.Fatalf("%+v", results)
	}
}

func executionFixture(t *testing.T, opts ...TaskOption) (*workerRuntime, *store.Queue, store.Delivery, *Job) {
	t.Helper()
	r := testRedis(t)
	ns := fmt.Sprintf("protocol-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	e, err := newWorkerRuntime(r, WorkerConfig{Namespace: ns, Retention: RetentionConfig{Succeeded: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	task := mustTask[int]("protocol", opts...)
	j, err := mustBind(e, task).Enqueue(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	q := e.queues[DefaultQueue].store
	if err := q.EnsureConsumerGroup(context.Background()); err != nil {
		t.Fatal(err)
	}
	ds, err := q.ReadDeliveries(context.Background(), "reader", time.Millisecond, 1)
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %v", ds, err)
	}
	return e, q, ds[0], j
}

func claimFixture(t *testing.T, q *store.Queue, d store.Delivery, token string) *Job {
	t.Helper()
	j, err := q.ClaimJob(
		context.Background(),
		d,
		store.ClaimCommand{
			Token:              token,
			Worker:             "worker",
			Duration:           time.Minute,
			ExpiredRetention:   time.Hour,
			CancelledRetention: time.Hour,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func expireFixture(t *testing.T, q *store.Queue, d store.Delivery) {
	t.Helper()
	if err := q.Client.HSet(context.Background(), q.Keys.Job(d.JobID), "lease_until", 0).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryBudgetIsIndependentAndAtomicallyCleansPEL(t *testing.T) {
	e, q, d, _ := executionFixture(t, WithRetry(NoRetry{}), WithRecoveryLimit(1))
	j := claimFixture(t, q, d, "first")
	if j.Attempt != 1 {
		t.Fatal(j)
	}
	expireFixture(t, q, d)
	j = claimFixture(t, q, d, "second")
	if j.RecoveryCount != 1 || j.Attempt != 2 || j.FailureCount != 0 {
		t.Fatal(j)
	}
	expireFixture(t, q, d)
	if _, err := q.ClaimJob(context.Background(), d, store.ClaimCommand{
		Token:    "third",
		Worker:   "worker",
		Duration: time.Minute,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
	stored, err := e.Jobs().Get(context.Background(), d.JobID)
	if err != nil || stored.State != JobDead || stored.Attempt != 2 {
		t.Fatalf("%+v %v", stored, err)
	}
	stats, err := q.Stats(context.Background())
	if err != nil || stats.Pending != 0 || stats.Running != 0 || stats.Dead != 1 {
		t.Fatalf("%+v %v", stats, err)
	}
}

func TestTotalExecutionBudgetAndExpiredLeaseCannotCommit(t *testing.T) {
	_, q, d, _ := executionFixture(t, WithExecutionLimit(1))
	claimFixture(t, q, d, "old")
	expireFixture(t, q, d)
	if err := q.CompleteJob(context.Background(), d, "old", time.Minute, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatal(err)
	}
	if _, err := q.ClaimJob(context.Background(), d, store.ClaimCommand{
		Token:    "replacement",
		Duration: time.Minute,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatal(err)
	}
}

func TestFinishReceiptReplaysAndRejectsConflictingRequest(t *testing.T) {
	e, q, d, _ := executionFixture(t)
	claimFixture(t, q, d, "token")
	ctx := context.Background()
	if err := q.CompleteJob(ctx, d, "token", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Jobs().Get(ctx, d.JobID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := q.CompleteJob(ctx, d, "token", 0, time.Minute); err != nil {
		t.Fatalf("response replay: %v", err)
	}
	if err := q.CompleteJob(ctx, d, "token", time.Minute, time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := q.FailJob(ctx, d, "token", store.FailureCommand{
		Message:       "different",
		DeadRetention: time.Hour,
	}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestFailureReceiptDoesNotRetryTwice(t *testing.T) {
	e, q, d, _ := executionFixture(t)
	claimFixture(t, q, d, "token")
	cmd := store.FailureCommand{
		Message:            "temporary",
		Retry:              true,
		Delay:              time.Second,
		DeadRetention:      time.Hour,
		CancelledRetention: time.Minute,
	}
	for range 2 {
		if err := q.FailJob(context.Background(), d, "token", cmd); err != nil {
			t.Fatal(err)
		}
	}
	j, err := e.Jobs().Get(context.Background(), d.JobID)
	if err != nil || j.FailureCount != 1 || j.State != JobScheduled {
		t.Fatalf("%+v %v", j, err)
	}
}

func TestReleaseDeliveryChecksConsumerAndMakesPendingClaimable(t *testing.T) {
	_, q, d, _ := executionFixture(t)
	ctx := context.Background()
	wrong := d
	wrong.Consumer = "other"
	if err := q.ReleaseDeliveries(ctx, []store.Delivery{wrong}); err != nil {
		t.Fatal(err)
	}
	ds, _, err := q.RecoverDeliveries(ctx, "replacement", time.Hour, "0-0", 1)
	if err != nil || len(ds) != 0 {
		t.Fatalf("%+v %v", ds, err)
	}
	if err := q.ReleaseDeliveries(ctx, []store.Delivery{d}); err != nil {
		t.Fatal(err)
	}
	ds, _, err = q.RecoverDeliveries(ctx, "replacement", time.Hour, "0-0", 1)
	if err != nil || len(ds) != 1 {
		t.Fatalf("%+v %v", ds, err)
	}
	// A stale owner cannot release the replacement's delivery.
	if err := q.ReleaseDeliveries(ctx, []store.Delivery{d}); err != nil {
		t.Fatal(err)
	}
	ds, _, err = q.RecoverDeliveries(ctx, "third", time.Hour, "0-0", 1)
	if err != nil || len(ds) != 0 {
		t.Fatalf("%+v %v", ds, err)
	}
}

func TestTimeoutDoesNotFreeActualHandlerCapacity(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("capacity-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	e := queueTestScheduler(t, r, ns, RoleCombined, DefaultQueue)
	task := mustTask[int]("blocked", WithTimeout(20*time.Millisecond), WithRetry(NoRetry{}))
	var calls atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	if err := mustBind(e, task).Handle(func(context.Context, int) error {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	defer func() { close(release); cancel(); <-done }()
	producer := mustBind(e, task)
	if _, err := producer.Enqueue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	if _, err := producer.Enqueue(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("capacity exceeded: %d", calls.Load())
	}
}

// A server version probe uses no mutation and must reject before enqueue.
type oldVersionHook struct{ commandCounter }

func (h *oldVersionHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "info" {
			cmd.(*redis.StringCmd).SetVal("redis_version:8.8.0\r\n")
			return nil
		}
		return next(ctx, cmd)
	}
}

func TestOldRedisRejectedBeforeMutation(t *testing.T) {
	r := testRedis(t)
	r.AddHook(&oldVersionHook{})
	ns := fmt.Sprintf("old-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	c, err := NewClient(r, ClientConfig{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Enqueue(context.Background(), mustTask[int]("old"), 1, WithJobID("probe")); !errors.Is(err, ErrUnsupportedRedis) {
		t.Fatal(err)
	}
	if n := r.Exists(context.Background(), c.state.namespace.Keys.Job("probe"), c.state.namespace.Keys.Submission("probe")).Val(); n != 0 {
		t.Fatal("unexpected data")
	}
}

func TestReadGroupClaimRecognizesRecoveredDelivery(t *testing.T) {
	_, q, d, _ := executionFixture(t)
	if err := q.Client.Do(context.Background(), "XCLAIM", q.Keys.Ready(), q.Keys.Group(), "reader", 0, d.MessageID, "IDLE", 60000).Err(); err != nil {
		t.Fatal(err)
	}
	ds, err := q.ReadDeliveries(context.Background(), "replacement", time.Millisecond, 1)
	if err != nil || len(ds) != 1 || !ds[0].Recovered || ds[0].MessageID != d.MessageID {
		t.Fatalf("%+v %v", ds, err)
	}
}

func TestHeartbeatBatchIsBounded(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("bounded-heartbeat-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	e, err := newWorkerRuntime(r, WorkerConfig{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	const n = 600
	requests := make([]store.LeaseRequest, n)
	ctx := context.Background()
	// Missing jobs return independent lease-loss results without creating entities.
	for i := range requests {
		requests[i] = store.LeaseRequest{JobID: fmt.Sprint(i), Token: "token", Lease: time.Minute, Renew: true}
	}
	counter := &commandCounter{}
	r.AddHook(counter)
	results := e.namespace.HeartbeatBatch(ctx, requests)
	if len(results) != n {
		t.Fatal(len(results))
	}
	for _, result := range results {
		if result.OK || result.Err != nil {
			t.Fatalf("%+v", result)
		}
	}
	if counter.pipelines != 3 || len(counter.commands) != n {
		t.Fatalf("pipelines=%d commands=%d", counter.pipelines, len(counter.commands))
	}
}

func TestFrozenBuiltinPolicyIgnoresCallerMutation(t *testing.T) {
	retry := &Exponential{MaxAttempts: 2, Initial: time.Second}
	task := mustTask[int]("frozen", WithRetry(retry))
	retry.MaxAttempts = 100
	retry.Initial = time.Hour
	if policyAttemptLimit(task.policy) != 2 || task.policy.Retry.Exponential.Initial != time.Second {
		t.Fatal(task.policy)
	}
}

func TestPublicZeroValues(t *testing.T) {
	ctx := context.Background()
	var c Client
	var w Worker
	var task Task[int]
	if _, err := (&c).Enqueue(ctx, task, 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if err := (&w).Register(task, func(context.Context, int) error { return nil }); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := c.Jobs().Get(ctx, "id"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := c.Schedules().Create(ctx, task, 1, CronSpec{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if err := w.Run(ctx); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
}

// Inject a lost reply after the server has already committed the operation.
type lostReplyHook struct {
	commandCounter
	scriptHash string
	lost       atomic.Bool
}

func (h *lostReplyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil &&
			(cmd.Name() == "evalsha" ||
				cmd.Name() == "eval") &&
			len(cmd.Args()) > 1 &&
			cmd.Args()[1] == h.scriptHash &&
			h.lost.CompareAndSwap(false, true) {
			cmd.SetErr(errInjectedOutage)
			return errInjectedOutage
		}
		return err
	}
}

func TestCommittedCompletionWithLostReplyCanReplay(t *testing.T) {
	_, q, d, _ := executionFixture(t)
	claimFixture(t, q, d, "lost-reply")
	ctx := context.Background()
	// Ensure the command takes the EVALSHA branch before injecting a lost reply.
	if err := store.Complete.Load(ctx, q.Client).Err(); err != nil {
		t.Fatal(err)
	}
	hook := &lostReplyHook{scriptHash: store.Complete.Hash()}
	q.Client.(*redis.Client).AddHook(hook)
	if err := q.CompleteJob(ctx, d, "lost-reply", 0, time.Minute); !errors.Is(err, errInjectedOutage) {
		t.Fatalf("expected lost reply, got %v", err)
	}
	if err := q.CompleteJob(ctx, d, "lost-reply", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	if n := q.Client.XLen(ctx, q.Keys.Ready()).Val(); n != 0 {
		t.Fatalf("remaining delivery: %d", n)
	}
}

func TestWorkerShutdownReturnsBufferedDeliveries(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("shutdown-buffer-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	e, err := newWorkerRuntime(
		r,
		WorkerConfig{
			Namespace: ns,
			Queues: []QueueConfig{{
				Name:        DefaultQueue,
				Concurrency: 1,
				FetchBatch:  4,
				Prefetch:    4,
			}},
			Execution: ExecutionConfig{
				PollTimeout:     20 * time.Millisecond,
				ShutdownTimeout: time.Second,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	task := mustTask[int]("drain")
	started, release := make(chan struct{}), make(chan struct{})
	if err := mustBind(e, task).Handle(func(context.Context, int) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if result := mustBind(e, task).EnqueueBatch(context.Background(), []int{1, 2, 3, 4}); result[0].Err != nil {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		close(release)
		<-done
		t.Fatal("handler not started")
	}
	q := e.queues[DefaultQueue].store
	eventually(t, time.Second, func() bool {
		return q.Client.XPending(context.Background(), q.Keys.Ready(), q.Keys.Group()).Val().Count == 4
	})
	cancel()
	time.Sleep(30 * time.Millisecond)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ds, _, err := q.RecoverDeliveries(context.Background(), "replacement", time.Hour, "0-0", 10)
	if err != nil || len(ds) != 3 {
		t.Fatalf("unstarted deliveries not returned: %+v %v", ds, err)
	}
}

func TestBoundedAttemptHistoryIsAtomicAndIndependentOfJob(t *testing.T) {
	e, q, d, _ := executionFixture(t, WithRecoveryLimit(3))
	q.HistoryLimit = 2
	q.HistoryTTL = time.Minute
	ctx := context.Background()
	claimFixture(t, q, d, "one")
	expireFixture(t, q, d)
	claimFixture(t, q, d, "two")
	expireFixture(t, q, d)
	claimFixture(t, q, d, "three")
	if err := q.CompleteJob(ctx, d, "three", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Replayed result does not append another history entry.
	if err := q.CompleteJob(ctx, d, "three", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	rows, err := e.Jobs().Attempts(ctx, d.JobID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("%+v %v", rows, err)
	}
	if rows[0].Token != "three" || rows[0].Outcome != "succeeded" || rows[1].Token != "two" || rows[1].Outcome != "lease_lost" {
		t.Fatalf("%+v", rows)
	}
	if rows[0].StartedAt.IsZero() || rows[0].FinishedAt.Before(rows[0].StartedAt) {
		t.Fatal(rows[0])
	}
	ttl, err := q.Client.PTTL(ctx, q.Keys.History(d.JobID)).Result()
	if err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("ttl=%v %v", ttl, err)
	}
	if _, err := e.Jobs().Get(ctx, d.JobID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAttemptHistoryDisabledAndFailureRetained(t *testing.T) {
	e, q, d, _ := executionFixture(t)
	ctx := context.Background()
	claimFixture(t, q, d, "disabled")
	if err := q.CompleteJob(ctx, d, "disabled", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	rows, err := e.Jobs().Attempts(ctx, d.JobID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("%+v %v", rows, err)
	}
	e, q, d, _ = executionFixture(t)
	q.HistoryLimit = 1
	q.HistoryTTL = time.Minute
	claimFixture(t, q, d, "failed")
	if err := q.FailJob(ctx, d, "failed", store.FailureCommand{
		Message:       "permanent",
		DeadRetention: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = e.Jobs().Attempts(ctx, d.JobID)
	if err != nil || len(rows) != 1 || rows[0].Outcome != "dead" || rows[0].Error != "permanent" {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestClientStateHasNoExecutionRegistry(t *testing.T) {
	r := testRedis(t)
	c, err := NewClient(r, ClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	typ := reflect.TypeFor[clientState]()
	for _, field := range []string{"active", "handlers", "workers", "dispatchers", "forceStop", "running", "engine"} {
		if _, ok := typ.FieldByName(field); ok {
			t.Errorf("Client retains runtime state: %s", field)
		}
	}
}
