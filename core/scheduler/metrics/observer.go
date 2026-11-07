// Package metrics adapts scheduler events to Prometheus collectors.
package metrics

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kochabx/kit/core/scheduler"
)

// Observer is both a scheduler.Observer and a prometheus.Collector.
// Register it once with a registry and pass it to Observability.Observer.
// Events are asynchronous; monitor Stats.ObserverDropped for collection losses.
type Observer struct {
	events      *prometheus.CounterVec
	durations   *prometheus.HistogramVec
	jobs        *prometheus.GaugeVec
	lag         *prometheus.GaugeVec
	pendingIdle *prometheus.GaugeVec
	dropped     prometheus.Gauge
}

func NewObserver() *Observer {
	return &Observer{
		jobs:        prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "scheduler_jobs", Help: "Latest externally supplied queue state snapshot."}, []string{"queue", "state"}),
		lag:         prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "scheduler_dispatch_lag_seconds", Help: "Latest delayed job dispatch lag."}, []string{"queue"}),
		pendingIdle: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "scheduler_pending_head_idle_seconds", Help: "Latest idle age of the first pending stream entry."}, []string{"queue"}),
		dropped:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "scheduler_observer_dropped", Help: "Latest process observer drop count supplied by the caller."}),
		events:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scheduler_events_total", Help: "Scheduler events by queue, task, kind and bounded outcome."}, []string{"queue", "task", "kind", "outcome"}),
		durations:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "scheduler_execution_duration_seconds", Help: "Handler execution duration including timeout waits.", Buckets: prometheus.DefBuckets}, []string{"queue", "task"}),
	}
}

func (o *Observer) Observe(_ context.Context, event scheduler.Event) {
	switch event.Kind {
	case scheduler.EventEnqueued, scheduler.EventStarted, scheduler.EventFinished, scheduler.EventRetried, scheduler.EventRecovered, scheduler.EventLeaseLost:
	default:
		return
	}
	outcome := "ok"
	switch {
	case event.CommitErr != nil:
		outcome = "commit_failed"
	case errors.Is(event.Err, scheduler.ErrJobCancelled):
		outcome = "cancelled"
	case errors.Is(event.Err, scheduler.ErrLeaseLost):
		outcome = "lease_lost"
	case event.Err != nil:
		outcome = "failed"
	}
	o.events.WithLabelValues(event.Queue, event.TaskName, string(event.Kind), outcome).Inc()
	if event.Kind == scheduler.EventFinished {
		o.durations.WithLabelValues(event.Queue, event.TaskName).Observe(event.Duration.Seconds())
	}
}
func (o *Observer) Describe(ch chan<- *prometheus.Desc) {
	o.events.Describe(ch)
	o.durations.Describe(ch)
	o.jobs.Describe(ch)
	o.lag.Describe(ch)
	o.pendingIdle.Describe(ch)
	o.dropped.Describe(ch)
}
func (o *Observer) Collect(ch chan<- prometheus.Metric) {
	o.events.Collect(ch)
	o.durations.Collect(ch)
	o.jobs.Collect(ch)
	o.lag.Collect(ch)
	o.pendingIdle.Collect(ch)
	o.dropped.Collect(ch)
}

// SetQueueStats publishes a caller-polled queue snapshot. Collect never contacts Redis.
func (o *Observer) SetQueueStats(queue string, stats scheduler.Stats) {
	for _, state := range []struct {
		name  string
		value int64
	}{{"scheduled", stats.Scheduled}, {"ready", stats.Ready}, {"pending", stats.Pending}, {"running", stats.Running}, {"dead", stats.Dead}, {"schedules", stats.Schedules}} {
		o.jobs.WithLabelValues(queue, state.name).Set(float64(state.value))
	}
	o.lag.WithLabelValues(queue).Set(stats.DispatchLag.Seconds())
	o.pendingIdle.WithLabelValues(queue).Set(stats.PendingHeadIdle.Seconds())
}

// SetObserverDropped publishes Client.Stats().ObserverDropped.
func (o *Observer) SetObserverDropped(count uint64) { o.dropped.Set(float64(count)) }
