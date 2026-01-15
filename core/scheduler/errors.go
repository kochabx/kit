package scheduler

import "errors"

var (
	// 任务相关错误
	ErrTaskNotFound      = errors.New("task not found")
	ErrTaskAlreadyExists = errors.New("task already exists")
	ErrInvalidTaskType   = errors.New("invalid task type")
	ErrInvalidPriority   = errors.New("invalid priority")
	ErrInvalidTimeout    = errors.New("invalid timeout")
	ErrInvalidMaxRetry   = errors.New("invalid max retry")
	ErrTaskCancelled     = errors.New("task cancelled")
	ErrTaskTimeout       = errors.New("task timeout")
	ErrTaskDuplicate     = errors.New("task duplicate")

	// Handler相关错误
	ErrHandlerNotFound = errors.New("handler not found")
	ErrHandlerPanic    = errors.New("handler panic")

	// Worker相关错误
	ErrWorkerNotFound = errors.New("worker not found")
	ErrAcquireLock    = errors.New("failed to acquire lock")
	ErrReleaseLock    = errors.New("failed to release lock")

	// 队列相关错误
	ErrQueueFull  = errors.New("queue is full")
	ErrQueueEmpty = errors.New("queue is empty")

	// 系统相关错误
	ErrRedisConnection    = errors.New("redis connection error")
	ErrShutdown           = errors.New("scheduler is shutting down")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

	// 配置相关错误
	ErrInvalidConfig    = errors.New("invalid configuration")
	ErrMissingNamespace = errors.New("namespace is required")
	ErrInvalidCron      = errors.New("invalid cron expression")
)
