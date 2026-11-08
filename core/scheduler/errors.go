package scheduler

import (
	"errors"
	"github.com/kochabx/kit/core/scheduler/internal/model"
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

// Permanent marks an error as non-retryable.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

type permanentError struct{ err error }

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

func isPermanent(err error) bool {
	_, ok := errors.AsType[permanentError](err)
	return ok
}
