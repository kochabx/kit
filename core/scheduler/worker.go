package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kochabx/kit/log"
	"github.com/panjf2000/ants/v2"
)

// Worker 工作节点
type Worker struct {
	id        string
	scheduler *Scheduler
	wg        sync.WaitGroup
	taskCount atomic.Int64
	startTime time.Time
	logger    *log.Logger
	running   atomic.Bool

	// 任务缓冲
	taskBuffer chan *taskItem

	// 当前正在处理的任务
	currentTaskID atomic.Value // string

	// 协程池
	pool *ants.Pool
}

// taskItem 任务项
type taskItem struct {
	taskID   string
	priority Priority
	msgID    string
}

// NewWorker 创建Worker
func NewWorker(scheduler *Scheduler) *Worker {
	w := &Worker{
		id:         "worker-" + uuid.New().String()[:8],
		scheduler:  scheduler,
		startTime:  time.Now(),
		logger:     scheduler.logger,
		taskBuffer: make(chan *taskItem, taskBufferSize),
	}
	w.currentTaskID.Store("")

	// 创建协程池
	concurrency := scheduler.opts.Worker.Concurrency
	if concurrency <= 0 {
		concurrency = 5 // 默认并发5
	}
	pool, err := ants.NewPool(concurrency, ants.WithPreAlloc(true))
	if err != nil {
		scheduler.logger.Error().Err(err).Msg("failed to create worker pool, using default")
		pool, _ = ants.NewPool(concurrency)
	}
	w.pool = pool

	return w
}

// Start 启动Worker
func (w *Worker) Start(ctx context.Context) error {
	if !w.running.CompareAndSwap(false, true) {
		return fmt.Errorf("worker already running")
	}

	// 创建带worker_id的logger
	w.logger = &log.Logger{
		Logger: w.scheduler.logger.Logger.With().Str("worker_id", w.id).Logger(),
	}
	w.logger.Info().Msg("worker starting")

	// 设置队列的消费者名称
	w.scheduler.queue.SetConsumer(w.id)

	// 注册Worker
	if err := w.register(ctx); err != nil {
		w.logger.Error().Err(err).Msg("failed to register worker")
		return err
	}

	// 启动续约goroutine
	w.wg.Add(1)
	go w.renewLease(ctx)

	// 启动任务拉取goroutine (流水线第一阶段)
	w.wg.Add(1)
	go w.fetchLoop(ctx)

	// 启动任务处理goroutine (流水线第二阶段)
	w.wg.Add(1)
	go w.processLoop(ctx)

	return nil
}

// Stop 停止Worker
func (w *Worker) Stop(ctx context.Context) error {
	if !w.running.CompareAndSwap(true, false) {
		return nil
	}

	w.logger.Info().Msg("worker stopping")

	// 记录当前正在处理的任务
	if taskID := w.getCurrentTaskID(); taskID != "" {
		w.logger.Warn().
			Str("task_id", taskID).
			Msg("worker stopping with task in progress, task will be retried")
	}

	// 等待所有goroutine退出
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	// 等待优雅关闭
	select {
	case <-done:
		w.logger.Info().Msg("worker stopped")
	case <-time.After(w.scheduler.opts.Worker.ShutdownGracePeriod):
		w.logger.Warn().Msg("worker stop timeout")
		// 记录未完成的任务
		if taskID := w.getCurrentTaskID(); taskID != "" {
			w.logger.Error().
				Str("task_id", taskID).
				Msg("worker forced shutdown, task may be lost or duplicated")
		}
	}

	// 注销Worker
	if err := w.unregister(ctx); err != nil {
		w.logger.Error().Err(err).Msg("failed to unregister worker")
	}

	// 释放协程池
	w.pool.Release()
	w.logger.Info().Msg("worker pool released")

	return nil
}

// getCurrentTaskID 获取当前正在处理的任务ID
func (w *Worker) getCurrentTaskID() string {
	if taskID, ok := w.currentTaskID.Load().(string); ok {
		return taskID
	}
	return ""
}

// register 注册Worker到Redis
func (w *Worker) register(ctx context.Context) error {
	workerInfo := &WorkerInfo{
		ID:            w.id,
		StartTime:     w.startTime,
		TaskCount:     0,
		LastHeartbeat: time.Now(),
	}

	// 使用对象池构建key
	workerKey := w.buildWorkerKey()
	if err := w.scheduler.client.HSet(ctx, workerKey, workerInfo.ToMap()).Err(); err != nil {
		return err
	}

	if err := w.scheduler.client.Expire(ctx, workerKey, w.scheduler.opts.Worker.LeaseTTL).Err(); err != nil {
		return err
	}

	return nil
}

// unregister 注销Worker
func (w *Worker) unregister(ctx context.Context) error {
	workerKey := w.buildWorkerKey()
	return w.scheduler.client.Del(ctx, workerKey).Err()
}

// buildWorkerKey 构建worker key
func (w *Worker) buildWorkerKey() string {
	return w.scheduler.opts.Namespace + ":worker:" + w.id
}

// renewLease 续约Worker租约
func (w *Worker) renewLease(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.scheduler.opts.Worker.RenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.renew(ctx); err != nil {
				w.logger.Error().Err(err).Msg("failed to renew lease")
			}
		}
	}
}

// renew 执行续约
func (w *Worker) renew(ctx context.Context) error {
	workerKey := w.buildWorkerKey()

	// 使用Pipeline批量更新
	pipe := w.scheduler.client.Pipeline()
	pipe.HSet(ctx, workerKey,
		"last_heartbeat", time.Now().Unix(),
		"task_count", w.taskCount.Load(),
	)
	pipe.Expire(ctx, workerKey, w.scheduler.opts.Worker.LeaseTTL)

	_, err := pipe.Exec(ctx)
	return err
}

// fetchLoop 任务拉取循环 (流水线第一阶段)
func (w *Worker) fetchLoop(ctx context.Context) {
	defer w.wg.Done()
	defer close(w.taskBuffer) // 关闭缓冲通知处理线程退出

	w.logger.Info().Msg("worker fetch loop started")

	for {
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("fetch loop stopped: context cancelled")
			return
		default:
			// 从队列获取任务
			taskID, priority, msgID, err := w.scheduler.queue.PopReady(ctx, 1)
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					return
				}
				w.logger.Error().Err(err).Msg("failed to pop ready task")
				continue
			}

			if taskID == "" {
				continue // 超时，无任务
			}

			// 发送到缓冲
			select {
			case w.taskBuffer <- &taskItem{taskID: taskID, priority: priority, msgID: msgID}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// processLoop 任务处理循环 (流水线第二阶段)
func (w *Worker) processLoop(ctx context.Context) {
	defer w.wg.Done()

	w.logger.Info().Msg("worker process loop started")

	for {
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("worker process loop stopped: context cancelled")
			return
		case item, ok := <-w.taskBuffer:
			if !ok {
				w.logger.Info().Msg("task buffer closed, exiting")
				return
			}
			// 使用协程池并发处理任务
			err := w.pool.Submit(func() {
				w.handleTask(ctx, item)
			})
			if err != nil {
				w.logger.Error().Err(err).Str("task_id", item.taskID).Msg("failed to submit task to pool")
				// 如果提交失败，同步处理
				w.handleTask(ctx, item)
			}
		}
	}
}

// handleTask 处理单个任务（在协程池中执行）
func (w *Worker) handleTask(ctx context.Context, item *taskItem) {
	// 处理任务
	err := w.processTask(ctx, item.taskID)

	// ACK/NACK
	if err == nil {
		if ackErr := w.scheduler.queue.AckMessage(ctx, item.priority, item.msgID); ackErr != nil {
			w.logger.Error().Err(ackErr).Str("task_id", item.taskID).Msg("failed to ack")
		}
	} else {
		w.logger.Error().Err(err).Str("task_id", item.taskID).Msg("task processing failed")
	}
}

// processTask 处理单个任务
func (w *Worker) processTask(ctx context.Context, taskID string) error {
	startTime := time.Now()

	// 记录当前处理的任务
	w.currentTaskID.Store(taskID)
	defer w.currentTaskID.Store("")

	// 尝试获取分布式锁
	lockStart := time.Now()
	acquired, err := w.scheduler.lock.Acquire(ctx, taskID, w.id, w.scheduler.opts.LockTimeout)
	if err != nil {
		w.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to acquire lock")
		return err
	}

	// 记录锁等待时间
	if w.scheduler.metrics.enabled {
		lockWaitDuration := time.Since(lockStart).Seconds()
		w.scheduler.metrics.RecordLockWait("task", lockWaitDuration)
		w.scheduler.metrics.RecordLockAcquired("task", acquired)
	}

	if !acquired {
		w.logger.Debug().Str("task_id", taskID).Msg("failed to acquire lock, task may be processing by another worker")
		return ErrAcquireLock
	}

	// 确保最后释放锁
	defer func() {
		if _, err := w.scheduler.lock.Release(ctx, taskID, w.id); err != nil {
			w.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to release lock")
		}
	}()

	// 获取任务信息
	taskInfo, err := w.scheduler.GetTaskInfo(ctx, taskID)
	if err != nil {
		w.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to get task info")
		return fmt.Errorf("get task info: %w", err)
	}

	// 更新任务状态为running
	now := time.Now()
	taskInfo.Status = StatusRunning
	taskInfo.WorkerID = w.id
	taskInfo.StartTime = &now
	if err := w.scheduler.saveTaskInfo(ctx, taskInfo); err != nil {
		w.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status")
		return fmt.Errorf("update task status: %w", err)
	}

	// 获取任务处理器
	handler, err := w.scheduler.registry.Get(taskInfo.Type)
	if err != nil {
		w.logger.Error().Err(err).Str("task_id", taskID).Str("type", taskInfo.Type).Msg("handler not found")
		w.handleTaskFailure(ctx, taskInfo, ErrHandlerNotFound)
		return ErrHandlerNotFound
	}

	// 创建带超时的context
	taskCtx, cancel := context.WithTimeout(ctx, taskInfo.Timeout)
	defer cancel()

	// 执行任务（带panic恢复）
	var execErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
				w.logger.Error().Str("task_id", taskID).Str("type", taskInfo.Type).Interface("panic", r).Msg("handler panic")
			}
		}()

		// 调用 handler.handle (闭包函数)
		execErr = handler.handle(taskCtx, taskInfo.Payload)
	}()

	// 计算执行时长
	executionTime := time.Since(startTime)
	taskInfo.ExecutionTime = &executionTime

	// 处理执行结果
	if execErr != nil {
		if execErr == context.DeadlineExceeded {
			w.logger.Warn().Str("task_id", taskID).Str("type", taskInfo.Type).Dur("timeout", taskInfo.Timeout).Msg("task timeout")
			execErr = ErrTaskTimeout
		}
		w.handleTaskFailure(ctx, taskInfo, execErr)
	} else {
		w.handleTaskSuccess(ctx, taskInfo)
	}

	// 增加任务计数
	w.taskCount.Add(1)

	// 记录Worker任务指标
	if w.scheduler.metrics.enabled {
		w.scheduler.metrics.RecordWorkerTask(w.id)
	}

	return nil
}

// handleTaskSuccess 处理任务成功
func (w *Worker) handleTaskSuccess(ctx context.Context, taskInfo *TaskInfo) {
	w.logger.Info().
		Str("task_id", taskInfo.ID).
		Str("type", taskInfo.Type).
		Str("duration", taskInfo.ExecutionTime.String()).
		Msg("task succeeded")

	now := time.Now()
	taskInfo.Status = StatusSuccess
	taskInfo.FinishTime = &now

	// 删除任务信息
	if err := w.scheduler.deleteTaskInfo(ctx, taskInfo.ID); err != nil {
		w.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to delete task info")
	}

	// 记录指标
	if w.scheduler.metrics.enabled {
		w.scheduler.metrics.RecordTaskExecuted(
			taskInfo.Type,
			StatusSuccess,
			taskInfo.ExecutionTime.Seconds(),
		)
	}

	// 如果是Cron任务，计算下次执行时间
	if taskInfo.Cron != "" {
		w.scheduler.scheduleNextCron(ctx, taskInfo)
	}
}

// handleTaskFailure 处理任务失败
func (w *Worker) handleTaskFailure(ctx context.Context, taskInfo *TaskInfo, err error) {
	w.logger.Warn().
		Str("task_id", taskInfo.ID).
		Str("type", taskInfo.Type).
		Int("retry_count", taskInfo.RetryCount).
		Err(err).
		Msg("task failed")

	taskInfo.RetryCount++
	taskInfo.LastError = err.Error()

	// 记录指标
	if w.scheduler.metrics.enabled {
		w.scheduler.metrics.RecordTaskExecuted(
			taskInfo.Type,
			StatusFailed,
			taskInfo.ExecutionTime.Seconds(),
		)
		w.scheduler.metrics.RecordTaskRetry(taskInfo.Type, taskInfo.RetryCount)
	}

	// 检查是否需要重试
	if taskInfo.RetryCount < taskInfo.MaxRetry {
		// 计算重试延迟
		retryDelay := w.scheduler.retryStrategy.NextRetry(taskInfo.RetryCount)

		w.logger.Info().
			Str("task_id", taskInfo.ID).
			Int("retry_count", taskInfo.RetryCount).
			Dur("delay", retryDelay).
			Msg("scheduling task retry")

		// 重新加入延迟队列
		taskInfo.Status = StatusPending
		taskInfo.ScheduleAt = time.Now().Add(retryDelay)
		taskInfo.StartTime = nil
		taskInfo.FinishTime = nil

		if err := w.scheduler.saveTaskInfo(ctx, taskInfo); err != nil {
			w.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to save task info")
		}

		// 加入延迟队列
		if err := w.scheduler.queue.AddDelayed(ctx, taskInfo.ID, float64(taskInfo.ScheduleAt.Unix())); err != nil {
			w.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to add task to delayed queue")
		}
	} else {
		// 超过最大重试次数，加入死信队列
		w.logger.Error().
			Str("task_id", taskInfo.ID).
			Str("type", taskInfo.Type).
			Int("retry_count", taskInfo.RetryCount).
			Msg("task exceeded max retries, moving to DLQ")

		now := time.Now()
		taskInfo.Status = StatusDead
		taskInfo.FinishTime = &now

		// 删除任务信息
		if err := w.scheduler.deleteTaskInfo(ctx, taskInfo.ID); err != nil {
			w.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to delete task info")
		}

		// 加入死信队列
		if err := w.scheduler.dlq.Add(ctx, taskInfo.ID); err != nil {
			w.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to add task to DLQ")
		}

		// 更新死信队列指标
		if w.scheduler.metrics.enabled {
			count, _ := w.scheduler.dlq.Count(ctx)
			w.scheduler.metrics.RecordDeadLetterCount(float64(count))
		}
	}
}
