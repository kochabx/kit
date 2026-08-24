package cx

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
)

// RunnerFunc adapts a function to Runnable.
type RunnerFunc func(context.Context) error

func (f RunnerFunc) Run(ctx context.Context) error {
	return f(ctx)
}

type execution struct {
	cancel   context.CancelFunc
	done     chan struct{}
	err      error
	stopping bool
}

// Runner adapts a blocking Runnable into a supervised lifecycle component.
// Stop is retryable after a timeout and executions never share mutable state.
type Runner struct {
	runnable Runnable
	mu       sync.RWMutex
	current  *execution
}

func NewRunner(runnable Runnable) (*Runner, error) {
	if isNil(runnable) {
		return nil, ErrNilRunnable
	}
	return &Runner{runnable: runnable}, nil
}

func MustRunner(runnable Runnable) *Runner {
	runner, err := NewRunner(runnable)
	if err != nil {
		panic(err)
	}
	return runner
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (r *Runner) Start(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		return ErrRunnerAlreadyStarted
	}
	runCtx, cancel := context.WithCancel(ctx)
	exec := &execution{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	r.current = exec

	go func() {
		err := r.run(runCtx)

		r.mu.Lock()
		exec.err = err
		r.mu.Unlock()

		close(exec.done)
	}()

	return nil
}

func (r *Runner) run(ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrRunnerPanic, recovered, debug.Stack())
		}
	}()

	return r.runnable.Run(ctx)
}

func (r *Runner) Stop(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	r.mu.Lock()
	exec := r.current
	if exec == nil {
		r.mu.Unlock()
		return nil
	}
	exec.stopping = true
	exec.cancel()
	r.mu.Unlock()

	select {
	case <-exec.done:
		r.mu.Lock()
		if r.current == exec {
			r.current = nil
		}
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) Done() <-chan struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return r.current.done
}

func (r *Runner) Err() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil {
		return ErrRunnerNotRunning
	}
	select {
	case <-r.current.done:
		if r.current.stopping && errors.Is(r.current.err, context.Canceled) {
			return nil
		}
		return r.current.err
	default:
		return nil
	}
}

func (r *Runner) HealthCheck(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil || r.current.stopping {
		return ErrRunnerNotRunning
	}
	select {
	case <-r.current.done:
		if r.current.err != nil {
			return errors.Join(ErrRunnerExited, r.current.err)
		}
		return ErrRunnerExited
	default:
		return nil
	}
}
