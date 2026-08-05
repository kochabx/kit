package scheduler

import "errors"

var (
	ErrClosed             = errors.New("scheduler is closed")
	ErrAlreadyRun         = errors.New("scheduler is already running")
	ErrNotFound           = errors.New("job not found")
	ErrDuplicate          = errors.New("unique job already exists")
	ErrInvalidState       = errors.New("job state does not allow this operation")
	ErrLeaseLost          = errors.New("job execution lease lost")
	ErrNoHandler          = errors.New("job handler is not registered")
	ErrShutdownTimeout    = errors.New("scheduler shutdown timed out")
	ErrJobCancelled       = errors.New("job cancellation requested")
	ErrDefinitionMismatch = errors.New("job definition does not match registered handler")
)

// Permanent marks an error as non-retryable.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func isPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}
