package scheduler

import (
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/model"
)

type JobState = model.JobState

type Job = model.Job

const (
	JobScheduled = model.JobScheduled
	JobReady     = model.JobReady
	JobRunning   = model.JobRunning
	JobSucceeded = model.JobSucceeded
	JobDead      = model.JobDead
	JobCancelled = model.JobCancelled
	JobExpired   = model.JobExpired
)

type EnqueueOption func(*enqueueOptions)
type enqueueOptions struct {
	id           string
	runAt        time.Time
	delay        time.Duration
	uniqueKey    string
	uniqueTTL    time.Duration
	expiresAt    time.Time
	expiresAfter time.Duration
}

func At(t time.Time) EnqueueOption {
	return func(o *enqueueOptions) { o.runAt, o.delay = t, 0 }
}

func Delay(d time.Duration) EnqueueOption {
	return func(o *enqueueOptions) { o.runAt, o.delay = time.Time{}, d }
}

func Unique(key string, ttl time.Duration) EnqueueOption {
	return func(o *enqueueOptions) { o.uniqueKey, o.uniqueTTL = key, ttl }
}

func ExpiresAt(t time.Time) EnqueueOption {
	return func(o *enqueueOptions) { o.expiresAt, o.expiresAfter = t, 0 }
}

func ExpiresAfter(d time.Duration) EnqueueOption {
	return func(o *enqueueOptions) { o.expiresAt, o.expiresAfter = time.Time{}, d }
}

// WithJobID makes submission retryable within Retention.Submission, even if the Job
// has already been removed. Reusing an ID with a different request is a conflict.
func WithJobID(id string) EnqueueOption {
	return func(o *enqueueOptions) { o.id = id }
}

// Attempt is an optional retained execution-history entry.
type Attempt = model.Attempt
