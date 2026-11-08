package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func TestRecreatedJobIDDoesNotReuseStreamIdempotencyIdentity(t *testing.T) {
	e, q, d, _ := executionFixture(t)
	ctx := context.Background()
	claimFixture(t, q, d, "first")
	if err := q.CompleteJob(ctx, d, "first", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Receipt expiry is independent of the still-live Stream IDMP window.
	if err := q.Client.Del(ctx, q.Keys.Submission(d.JobID)).Err(); err != nil {
		t.Fatal(err)
	}
	task := mustTask[int]("protocol")
	if _, err := mustBind(e, task).Enqueue(ctx, 2, WithJobID(d.JobID)); err != nil {
		t.Fatal(err)
	}
	ds, err := q.ReadDeliveries(ctx, "second", time.Millisecond, 1)
	if err != nil || len(ds) != 1 {
		t.Fatalf("Job recreated without delivery: %+v %v", ds, err)
	}
	claimFixture(t, q, ds[0], "second")
}

func TestClaimReplayDoesNotConsumeAttemptOrRecoveryBudget(t *testing.T) {
	_, q, d, _ := executionFixture(t)
	ctx := context.Background()
	// Actual response loss after the server commits the claim.
	if err := store.Claim.Load(ctx, q.Client).Err(); err != nil {
		t.Fatal(err)
	}
	q.Client.(*redis.Client).AddHook(&lostReplyHook{scriptHash: store.Claim.Hash()})
	cmd := store.ClaimCommand{Token: "stable", Worker: "worker", Duration: time.Minute}
	if _, err := q.ClaimJob(ctx, d, cmd); !errors.Is(err, errInjectedOutage) {
		t.Fatal(err)
	}
	j, err := q.ClaimJob(ctx, d, cmd)
	if err != nil || j.Attempt != 1 || j.RecoveryCount != 0 {
		t.Fatalf("%+v %v", j, err)
	}
	firstStart := j.StartedAt
	j, err = q.ClaimJob(ctx, d, cmd)
	if err != nil || !j.StartedAt.Equal(firstStart) || j.Attempt != 1 {
		t.Fatalf("%+v %v", j, err)
	}
	cmd.Worker = "other"
	if _, err := q.ClaimJob(ctx, d, cmd); !errors.Is(err, ErrLeaseLost) {
		t.Fatal(err)
	}
}

func TestReceiptRetentionIsIndependent(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("receipt-ttl-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	e, err := newWorkerRuntime(r, WorkerConfig{
		Namespace: ns,
		Retention: RetentionConfig{
			Submission: time.Hour,
			Completion: time.Minute,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	q := e.queues[DefaultQueue].store
	ctx := context.Background()
	j, err := mustBind(e, mustTask[int]("ttl")).Enqueue(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	ds, err := q.ReadDeliveries(ctx, "reader", time.Millisecond, 1)
	if err != nil || len(ds) != 1 {
		t.Fatal(err)
	}
	claimFixture(t, q, ds[0], "token")
	if err := q.CompleteJob(ctx, ds[0], "token", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	sub := r.PTTL(ctx, q.Keys.Submission(j.ID)).Val()
	comp := r.PTTL(ctx, q.Keys.Receipt(j.ID, "token")).Val()
	if sub < 59*time.Minute || sub > time.Hour || comp < 50*time.Second || comp > time.Minute {
		t.Fatalf("submission=%v completion=%v", sub, comp)
	}
}

func TestRejectedJobReportsCommitFailure(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("reject-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	events := make(chan Event, 8)
	e, err := newWorkerRuntime(
		r,
		WorkerConfig{
			Namespace: ns,
			Observability: ObservabilityConfig{Observer: ObserverFunc(func(_ context.Context, event Event) {
				events <- event
			})},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	producer := mustTask[int]("reject", WithRetry(panicRetry{}))
	worker := mustTask[int]("reject")
	if err := mustBind(e, worker).Handle(func(context.Context, int) error {
		t.Error("unexpected execution")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	q := e.queues[DefaultQueue].store
	ctx := context.Background()
	if _, err := mustBind(e, producer).Enqueue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := q.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	ds, err := q.ReadDeliveries(ctx, "reader", time.Millisecond, 1)
	if err != nil || len(ds) != 1 {
		t.Fatal(err)
	}
	if err := store.Fail.Load(ctx, r).Err(); err != nil {
		t.Fatal(err)
	}

	r.AddHook(&rejectFailureHook{})
	e.queues[DefaultQueue].execute(ctx, "worker", ds[0])
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == EventFinished {
				if !errors.Is(event.Err, ErrTaskMismatch) || !errors.Is(event.CommitErr, errInjectedOutage) {
					t.Fatalf("%+v", event)
				}
				return
			}
		case <-timer.C:
			t.Fatal("missing rejection event")
		}
	}
}

type rejectFailureHook struct{ commandCounter }

func (h *rejectFailureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "evalsha" && len(cmd.Args()) > 1 && cmd.Args()[1] == store.Fail.Hash() {
			cmd.SetErr(errInjectedOutage)
			return errInjectedOutage
		}
		return next(ctx, cmd)
	}
}

func TestRecoveryMakesProgressWithContinuousNewJobs(t *testing.T) {
	r := testRedis(t)
	ns := fmt.Sprintf("fair-recovery-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanup(t, r, ns) })
	seed, err := newWorkerRuntime(r, WorkerConfig{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	task := mustTask[int]("fair", WithRetry(NoRetry{}))
	ctx := context.Background()
	target, err := mustBind(seed, task).Enqueue(ctx, -1)
	if err != nil {
		t.Fatal(err)
	}
	q := seed.queues[DefaultQueue].store
	if err := q.EnsureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	ds, err := q.ReadDeliveries(ctx, "failed-node", time.Millisecond, 1)
	if err != nil || len(ds) != 1 {
		t.Fatal(err)
	}
	claimFixture(t, q, ds[0], "old")
	expireFixture(t, q, ds[0])
	if err := r.Do(ctx, "XCLAIM", q.Keys.Ready(), q.Keys.Group(), "failed-node", 0, ds[0].MessageID, "IDLE", 60000).Err(); err != nil {
		t.Fatal(err)
	}
	for i := range 200 {
		if _, err := mustBind(seed, task).Enqueue(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 2)
	for range 2 {
		w, err := NewWorker(
			r,
			WorkerConfig{
				Namespace: ns,
				Queues: []QueueConfig{{
					Name:        DefaultQueue,
					Concurrency: 2,
					FetchBatch:  4,
				}},
				Execution: ExecutionConfig{
					PollTimeout:        20 * time.Millisecond,
					LeaseDuration:      3 * time.Second,
					LeaseRenewInterval: 100 * time.Millisecond,
				},
				Retention: RetentionConfig{Succeeded: time.Minute},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Register(task, func(ctx context.Context, _ int) error {
			select {
			case <-time.After(time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}); err != nil {
			t.Fatal(err)
		}
		go func() { done <- w.Run(run) }()
	}
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				_, _ = mustBind(seed, task).Enqueue(run, 1)
			}
		}
	}()
	defer func() {
		cancel()
		<-producerDone
		for range 2 {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	}()
	eventually(t, 5*time.Second, func() bool {
		j, e := seed.Jobs().Get(ctx, target.ID)
		return e == nil && j.State == JobSucceeded && j.RecoveryCount == 1
	})
}

func BenchmarkReceiptFootprint(b *testing.B) {
	r := testRedis(b)
	ns := fmt.Sprintf("receipt-memory-%d", time.Now().UnixNano())
	b.Cleanup(func() { cleanup(b, r, ns) })
	e, err := newWorkerRuntime(r, WorkerConfig{Namespace: ns})
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	task := mustBind(e, mustTask[int]("memory"))
	q := e.queues[DefaultQueue].store
	ctx := context.Background()
	if err := q.EnsureConsumerGroup(ctx); err != nil {
		b.Fatal(err)
	}
	var sampleID string
	b.ResetTimer()
	for b.Loop() {
		j, err := task.Enqueue(ctx, 1)
		if err != nil {
			b.Fatal(err)
		}
		sampleID = j.ID
		ds, err := q.ReadDeliveries(ctx, "bench", time.Millisecond, 1)
		if err != nil || len(ds) != 1 {
			b.Fatal(err)
		}
		token := j.ID
		if _, err := q.ClaimJob(ctx, ds[0], store.ClaimCommand{
			Token:    token,
			Worker:   "bench",
			Duration: time.Minute,
		}); err != nil {
			b.Fatal(err)
		}
		if err := q.CompleteJob(ctx, ds[0], token, 0, time.Minute); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	sub, err := r.MemoryUsage(ctx, q.Keys.Submission(sampleID)).Result()
	if err != nil {
		b.Fatal(err)
	}
	comp, err := r.MemoryUsage(ctx, q.Keys.Receipt(sampleID, sampleID)).Result()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(sub), "submission-B/job")
	b.ReportMetric(float64(comp), "completion-B/job")
	b.ReportMetric(2, "receipt-keys/job")
}
