package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kochabx/kit/core/scheduler"
)

func TestObserverCollectsBoundedOutcomes(t *testing.T) {
	observer := NewObserver()
	registry := prometheus.NewRegistry()
	if err := registry.Register(observer); err != nil {
		t.Fatal(err)
	}
	for _, event := range []scheduler.Event{
		{Kind: scheduler.EventEnqueued}, {Kind: scheduler.EventStarted},
		{Kind: scheduler.EventFinished, Duration: time.Second},
		{Kind: scheduler.EventFinished, CommitErr: errors.New("storage"), Duration: 2 * time.Second},
		{Kind: scheduler.EventRetried, Err: errors.New("temporary")},
		{Kind: scheduler.EventRecovered}, {Kind: scheduler.EventLeaseLost, Err: scheduler.ErrLeaseLost},
		{Kind: scheduler.EventKind("arbitrary")},
	} {
		event.Queue = "mail"
		event.TaskName = "email"
		event.JobID = "unique-job"
		observer.Observe(context.Background(), event)
	}
	observer.SetQueueStats("mail", scheduler.Stats{
		Ready:           4,
		Pending:         2,
		DispatchLag:     time.Second,
		PendingHeadIdle: 2 * time.Second,
	})
	observer.SetObserverDropped(3)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	counters := float64(0)
	histograms := uint64(0)
	commitFailures := float64(0)
	for _, family := range families {
		for _, metric := range family.Metric {
			if family.GetName() == "scheduler_observer_dropped" && metric.GetGauge().GetValue() != 3 {
				t.Fatal("wrong drop gauge")
			}
			if family.GetName() == "scheduler_dispatch_lag_seconds" && metric.GetGauge().GetValue() != 1 {
				t.Fatal("wrong lag gauge")
			}
			if family.GetName() == "scheduler_pending_head_idle_seconds" && metric.GetGauge().GetValue() != 2 {
				t.Fatal("wrong pending idle gauge")
			}
			if family.GetName() == "scheduler_jobs" {
				for _, label := range metric.Label {
					if label.GetName() == "state" && label.GetValue() == "ready" && metric.GetGauge().GetValue() != 4 {
						t.Fatal("wrong ready gauge")
					}
				}
			}
			for _, label := range metric.Label {
				if label.GetName() == "job_id" || label.GetValue() == "unique-job" {
					t.Fatal("job ID in metrics")
				}
				if label.GetName() == "outcome" && label.GetValue() == "commit_failed" {
					commitFailures += metric.GetCounter().GetValue()
				}
			}
			if metric.Counter != nil {
				counters += metric.Counter.GetValue()
			}
			if metric.Histogram != nil {
				histograms += metric.Histogram.GetSampleCount()
				if metric.Histogram.GetSampleSum() != 3 {
					t.Fatal("wrong duration sum")
				}
			}
		}
	}
	if counters != 7 || histograms != 2 || commitFailures != 1 {
		t.Fatalf("events=%v histograms=%d commit failures=%v", counters, histograms, commitFailures)
	}
}
