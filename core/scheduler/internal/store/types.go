package store

import "github.com/kochabx/kit/core/scheduler/internal/model"

type JobState = model.JobState
type Job = model.Job
type CronSpec = model.CronSpec
type ScheduleState = model.ScheduleState
type Schedule = model.Schedule
type ScheduleQuery = model.ScheduleQuery
type ExecutionPolicy = model.ExecutionPolicy
type RetrySpec = model.RetrySpec
type Stats = model.Stats
type Scan = model.Scan
type DeadQuery = model.DeadQuery
type EnqueueResult = model.EnqueueResult
type Exponential = model.Exponential
type Page[T any] = model.Page[T]

const (
	JobScheduled      = model.JobScheduled
	JobReady          = model.JobReady
	JobRunning        = model.JobRunning
	JobSucceeded      = model.JobSucceeded
	JobDead           = model.JobDead
	JobCancelled      = model.JobCancelled
	JobExpired        = model.JobExpired
	ScheduleActive    = model.ScheduleActive
	SchedulePaused    = model.SchedulePaused
	ScheduleCancelled = model.ScheduleCancelled
	ScheduleInvalid   = model.ScheduleInvalid
)

var (
	ErrUnsupportedRedis     = model.ErrUnsupportedRedis
	ErrConflict             = model.ErrConflict
	ErrTaskRegistered       = model.ErrTaskRegistered
	ErrUnknownQueue         = model.ErrUnknownQueue
	ErrClosed               = model.ErrClosed
	ErrAlreadyRun           = model.ErrAlreadyRun
	ErrNotFound             = model.ErrNotFound
	ErrDuplicate            = model.ErrDuplicate
	ErrInvalidState         = model.ErrInvalidState
	ErrInvalidArgument      = model.ErrInvalidArgument
	ErrLeaseLost            = model.ErrLeaseLost
	ErrMaintenanceLeaseLost = model.ErrMaintenanceLeaseLost
	ErrNoHandler            = model.ErrNoHandler
	ErrShutdownTimeout      = model.ErrShutdownTimeout
	ErrJobCancelled         = model.ErrJobCancelled
	ErrTaskMismatch         = model.ErrTaskMismatch
)
