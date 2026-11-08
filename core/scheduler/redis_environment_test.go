package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/redis/go-redis/v9"
	"os"
	"strings"
	"testing"
	"time"
)

// Invoked only by the isolated Docker harness, never against a shared deployment.
func TestIsolatedClusterFailover(t *testing.T) {
	addrs := os.Getenv("SCHEDULER_CLUSTER_ADDRS")
	if addrs == "" {
		t.Skip("use scripts/verify-redis.sh")
	}
	if os.Getenv("SCHEDULER_ISOLATED_REDIS") != "1" {
		t.Fatal("cluster mutation requires isolated harness")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r := redis.NewClusterClient(&redis.ClusterOptions{Addrs: strings.Split(addrs, ","), ContextTimeoutEnabled: true})
	defer r.Close()
	ns := fmt.Sprintf("cluster-%d", time.Now().UnixNano())
	c, err := NewClient(r, ClientConfig{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	task := mustTask[int]("cluster", WithRetry(NoRetry{}))
	j, err := c.Enqueue(ctx, task, 1, WithJobID("cluster-request"))
	if err != nil {
		t.Fatal(err)
	}
	key := c.state.namespace.Keys.Job(j.ID)
	slot, err := r.ClusterKeySlot(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	replica, master := "", ""
	// Cluster creation can finish before replicas become online and appear in SLOTS.
	eventually(t, 15*time.Second, func() bool {
		slots, e := r.ClusterSlots(ctx).Result()
		if e != nil {
			return false
		}
		for _, s := range slots {
			if int64(s.Start) <= slot && slot <= int64(s.End) && len(s.Nodes) > 1 {
				master = s.Nodes[0].Addr
				replica = s.Nodes[1].Addr
				return true
			}
		}
		return false
	})
	repl := redis.NewClient(&redis.Options{Addr: replica, ContextTimeoutEnabled: true, MaxRetries: -1})
	defer repl.Close()
	// Wait for the replica to acknowledge the write before testing role transfer.
	primary := redis.NewClient(&redis.Options{Addr: master, ContextTimeoutEnabled: true})
	defer primary.Close()
	if err := primary.Set(ctx, key+":replication-barrier", "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	n, err := primary.Wait(ctx, 1, 5*time.Second).Result()
	if err != nil || n < 1 {
		t.Fatalf("replication barrier: %d %v", n, err)
	}
	if err := repl.Do(ctx, "CLUSTER", "FAILOVER").Err(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, func() bool {
		info, e := repl.Info(ctx, "replication").Result()
		return e == nil && strings.Contains(info, "role:master")
	})
	r.ReloadState(ctx)
	eventually(t, 5*time.Second, func() bool { _, e := c.Jobs().Get(ctx, j.ID); return e == nil })
	if _, err := c.Enqueue(ctx, task, 1, WithJobID("cluster-request")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("receipt after failover: %v", err)
	}
	w, err := NewWorker(
		r,
		WorkerConfig{
			Namespace: ns,
			Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond},
			Retention: RetentionConfig{Succeeded: time.Minute},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Register(task, func(context.Context, int) error { return nil }); err != nil {
		t.Fatal(err)
	}
	run, cancelRun := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(run) }()
	defer func() {
		cancelRun()
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	eventually(t, 5*time.Second, func() bool {
		job, e := c.Jobs().Get(ctx, j.ID)
		return e == nil && job.State == JobSucceeded
	})

	// Terminate the promoted primary and verify automatic replacement, not only
	// a coordinated role transfer. This deployment is disposable and isolated.
	pending, err := c.Enqueue(ctx, task, 2, WithJobID("node-loss"), Delay(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := repl.Set(ctx, key+":node-loss-barrier", "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	n, err = repl.Wait(ctx, 1, 10*time.Second).Result()
	if err != nil || n < 1 {
		t.Fatalf("new replica not synchronized: %d %v", n, err)
	}
	if err := repl.ShutdownNoSave(ctx).Err(); err != nil {
		t.Fatalf("terminate isolated primary: %v", err)
	}
	eventually(t, 20*time.Second, func() bool {
		r.ReloadState(ctx)
		owner, e := r.MasterForKey(ctx, key)
		if e != nil || owner.Options().Addr == replica {
			return false
		}
		job, e := c.Jobs().Get(ctx, pending.ID)
		return e == nil && job.State == JobScheduled
	})
	if _, err := c.Enqueue(ctx, task, 2, WithJobID("node-loss"), Delay(time.Hour)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("receipt after primary loss: %v", err)
	}
}

// The harness stops and restarts the AOF-backed container between these phases.
func TestIsolatedPersistenceAcrossRestart(t *testing.T) {
	phase := os.Getenv("SCHEDULER_PERSISTENCE_PHASE")
	if phase == "" {
		t.Skip("use scripts/verify-redis.sh")
	}
	if os.Getenv("SCHEDULER_ISOLATED_REDIS") != "1" {
		t.Fatal("requires isolated harness")
	}
	r := redis.NewClient(&redis.Options{Addr: os.Getenv("SCHEDULER_PERSISTENCE_ADDR"), ContextTimeoutEnabled: true})
	defer r.Close()
	ctx := context.Background()
	ns := os.Getenv("SCHEDULER_PERSISTENCE_NAMESPACE")
	c, err := NewClient(r, ClientConfig{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	task := mustTask[int]("persist", WithRetry(NoRetry{}))
	if phase == "seed" {
		if _, err := c.Enqueue(ctx, task, 1, WithJobID("future"), Delay(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Schedules().Create(ctx, task, 2, CronSpec{Expression: "@every 1h"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Enqueue(ctx, task, 3, WithJobID("finished")); err != nil {
			t.Fatal(err)
		}
		w, err := NewWorker(
			r,
			WorkerConfig{
				Namespace: ns,
				Execution: ExecutionConfig{PollTimeout: 20 * time.Millisecond},
				History:   HistoryConfig{MaxEntries: 2},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Register(task, func(context.Context, int) error { return nil }); err != nil {
			t.Fatal(err)
		}
		run, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- w.Run(run) }()
		eventually(t, 5*time.Second, func() bool {
			rows, e := c.Jobs().Attempts(ctx, "finished")
			return e == nil && len(rows) == 1
		})
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		return
	}
	if phase != "verify" {
		t.Fatal("unknown phase")
	}
	j, err := c.Jobs().Get(ctx, "future")
	if err != nil || j.State != JobScheduled {
		t.Fatalf("delayed job: %+v %v", j, err)
	}
	if _, err := c.Enqueue(ctx, task, 3, WithJobID("finished")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("submission receipt: %v", err)
	}
	rows, err := c.Jobs().Attempts(ctx, "finished")
	if err != nil || len(rows) != 1 || rows[0].Outcome != "succeeded" {
		t.Fatalf("history: %+v %v", rows, err)
	}
	q, err := c.Queue(DefaultQueue)
	if err != nil {
		t.Fatal(err)
	}
	schedules, err := q.Schedules(ctx, ScheduleQuery{Count: 100})
	if err != nil || len(schedules.Items) != 1 || schedules.Items[0].State != ScheduleActive {
		t.Fatalf("schedule: %+v %v", schedules, err)
	}
	// Completion receipt survives independently of the already-deleted Job.
	queueStore := c.state.queueStores[DefaultQueue].store
	if err := queueStore.CompleteJob(ctx, store.Delivery{JobID: "finished"}, rows[0].Token, 0, time.Hour); err != nil {
		t.Fatalf("completion receipt: %v", err)
	}
}
