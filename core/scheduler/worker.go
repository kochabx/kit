package scheduler

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Worker owns execution, dispatch, leases, recovery, and maintenance.
// Register handlers before Run. The borrowed Redis client is never closed.
type Worker struct{ runtime *workerRuntime }

func NewWorker(redisClient redis.UniversalClient, config WorkerConfig) (*Worker, error) {
	e, err := newWorkerRuntime(redisClient, config)
	if err != nil {
		return nil, err
	}
	return &Worker{runtime: e}, nil
}
func (worker *Worker) Register[T any](task Task[T], fn Handler[T]) error {
	if worker == nil || worker.runtime == nil || task.fingerprint == "" {
		return ErrInvalidArgument
	}
	if _, err := worker.runtime.lookupQueue(task.queue); err != nil {
		return err
	}
	return worker.runtime.registerTaskHandler(task, fn)
}
func (w *Worker) Run(ctx context.Context) error {
	if w == nil || w.runtime == nil {
		return ErrInvalidArgument
	}
	return w.runtime.Run(ctx)
}

// Close releases a Worker that has not been run. Cancel Run's context to stop a running Worker.
func (w *Worker) Close() error {
	if w == nil || w.runtime == nil {
		return ErrInvalidArgument
	}
	return w.runtime.Close()
}
