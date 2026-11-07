package model

import "errors"

var (
	ErrUnsupportedRedis     = errors.New("unsupported Redis server capabilities")
	ErrConflict             = errors.New("idempotency key reused with different request")
	ErrTaskRegistered       = errors.New("task handler already registered")
	ErrUnknownQueue         = errors.New("queue is not configured")
	ErrClosed               = errors.New("scheduler is closed")
	ErrAlreadyRun           = errors.New("scheduler is already running")
	ErrNotFound             = errors.New("job not found")
	ErrDuplicate            = errors.New("unique job already exists")
	ErrInvalidState         = errors.New("job state does not allow this operation")
	ErrInvalidArgument      = errors.New("invalid scheduler argument")
	ErrLeaseLost            = errors.New("job execution lease lost")
	ErrMaintenanceLeaseLost = errors.New("scheduler maintenance lease lost")
	ErrNoHandler            = errors.New("job handler is not registered")
	ErrShutdownTimeout      = errors.New("scheduler shutdown timed out")
	ErrJobCancelled         = errors.New("job cancellation requested")
	ErrTaskMismatch         = errors.New("job task metadata does not match registered handler")
)
