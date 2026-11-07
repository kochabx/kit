package model

import "time"

type JobState string

type Job struct {
	Version       string
	Policy        ExecutionPolicy
	RecoveryCount int
	FailureCount  int
	Queue         string
	ID            string
	TaskName      string
	State         JobState
	Payload       []byte
	Attempt       int
	MaxAttempts   int
	RunAt         time.Time
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
	ExpiresAt     time.Time
	LastError     string
	UniqueKey     string
	Fingerprint   string
}

type EnqueueResult struct {
	Job *Job
	Err error
}

type Attempt struct {
	JobID      string
	Token      string
	WorkerID   string
	Number     int
	StartedAt  time.Time
	FinishedAt time.Time
	Outcome    string
	Error      string
}

const (
	JobScheduled JobState = "scheduled"
	JobReady     JobState = "ready"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobDead      JobState = "dead"
	JobCancelled JobState = "cancelled"
	JobExpired   JobState = "expired"
)
