package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kochabx/kit/core/rate"
	"github.com/kochabx/kit/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

const (
	// 缓存配置
	statsCacheSeconds = 1  // 统计信息缓存间隔（秒）
	taskBufferSize    = 10 // Worker任务缓冲区大小
	mapPoolInitialCap = 20 // map对象池初始容量

	// 超时配置
	idleTimeMultiplier    = 2   // Pending消息超时倍数
	workerScanBatchSize   = 100 // Worker扫描批次大小
	redisConnTestTimeout  = 5   // Redis连接测试超时（秒）
	reclaimPendingTimeout = 10  // 回收Pending消息超时（秒）
	mapPoolMaxSize        = 40  // map对象池最大容量（超过不放回）
)

// Scheduler 分布式任务调度器
type Scheduler struct {
	opts   *Options
	client *redis.Client

	// 核心组件
	registry      *Registry
	queue         QueueStore
	lock          LockProvider
	dedup         DeduplicationStore
	dlq           DeadLetterStore
	cronParser    *CronParser
	retryStrategy RetryStrategy

	// 保护组件
	rateLimiter    rate.Limiter
	circuitBreaker *CircuitBreaker

	// 监控组件
	metrics       *Metrics
	healthChecker *HealthChecker
	logger        *log.Logger

	// Worker管理
	workers []*Worker

	// 运行状态
	running atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// HTTP服务器
	metricsServer *http.Server
	healthServer  *http.Server

	mapPool       *sync.Pool   // map对象池
	statsCache    atomic.Value // 统计信息缓存 *QueueStats
	lastStatsTime atomic.Int64 // 上次更新统计的时间戳
}

// New 创建调度器
func New(opts ...Option) (*Scheduler, error) {
	// 应用选项
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	// 创建或使用Redis客户端
	var client *redis.Client
	if options.Redis.Client != nil {
		client = options.Redis.Client
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:         options.Redis.Addr,
			Password:     options.Redis.Password,
			DB:           options.Redis.DB,
			PoolSize:     options.Redis.PoolSize,
			MinIdleConns: options.Redis.MinIdleConns,
			MaxRetries:   options.Redis.MaxRetries,
			DialTimeout:  options.Redis.DialTimeout,
			ReadTimeout:  options.Redis.ReadTimeout,
			WriteTimeout: options.Redis.WriteTimeout,
			PoolTimeout:  options.Redis.PoolTimeout,
		})

		// 测试连接
		ctx, cancel := context.WithTimeout(context.Background(), redisConnTestTimeout*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("failed to connect to redis: %w", err)
		}
	}

	// 创建日志器
	logger := options.CustomLogger
	if logger == nil {
		// 使用项目全局日志记录器
		logger = log.G
	}

	// 创建调度器
	s := &Scheduler{
		opts:           options,
		client:         client,
		registry:       NewRegistry(),
		queue:          NewQueue(client, options.Namespace),
		lock:           NewDistLock(client, options.Namespace),
		dedup:          NewDeduplicator(client, options.Namespace, options.DedupEnabled, options.DedupDefaultTTL),
		dlq:            NewDeadLetterQueue(client, options.Namespace, options.DLQEnabled, options.DLQMaxSize),
		cronParser:     NewCronParser(),
		retryStrategy:  NewExponentialBackoff(options.Retry.BaseDelay, options.Retry.MaxDelay, options.Retry.Multiplier, options.Retry.Jitter),
		rateLimiter:    rate.NewTokenBucketLimiter(client, options.Namespace+":ratelimit", options.RateLimit.Burst, options.RateLimit.Rate),
		circuitBreaker: NewCircuitBreaker(options.CircuitBreaker.Enabled, options.CircuitBreaker.MaxFailures, options.CircuitBreaker.Timeout),
		metrics:        NewMetrics(options.Namespace, options.Metrics.Enabled),
		logger:         logger,
		mapPool: &sync.Pool{
			New: func() any {
				return make(map[string]any, mapPoolInitialCap)
			},
		},
	}

	// 创建健康检查器
	s.healthChecker = NewHealthChecker(s)

	// 创建Workers
	s.workers = make([]*Worker, options.Worker.Count)
	for i := 0; i < options.Worker.Count; i++ {
		s.workers[i] = NewWorker(s)
	}

	logger.Info().
		Str("namespace", options.Namespace).
		Int("worker_count", options.Worker.Count).
		Str("redis_addr", options.Redis.Addr).
		Msg("scheduler created")

	return s, nil
}

// SchedulerRegister 在 Scheduler 上注册泛型任务处理器
func SchedulerRegister[T any](s *Scheduler, taskType string, handler Handler[T]) error {
	return Register(s.registry, taskType, handler)
}

// SchedulerRegisterWithSerializer 使用指定序列化器在 Scheduler 上注册泛型任务处理器
func SchedulerRegisterWithSerializer[T any](s *Scheduler, taskType string, handler Handler[T], serializer Serializer) error {
	return RegisterWithSerializer(s.registry, taskType, handler, serializer)
}

// Registry 获取任务注册表
func (s *Scheduler) Registry() *Registry {
	return s.registry
}

// SetSerializer 设置默认序列化器
func (s *Scheduler) SetSerializer(serializer Serializer) {
	s.registry.SetSerializer(serializer)
}

// Start 启动调度器
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return fmt.Errorf("scheduler already running")
	}

	// 创建可取消的context
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.logger.Info().Msg("scheduler starting")

	// 启动Prometheus指标服务
	if s.opts.Metrics.Enabled {
		if err := s.startMetricsServer(); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}

	// 启动健康检查服务
	if s.opts.Health.Enabled {
		if err := s.healthChecker.Start(s.opts.Health.Port, s.opts.Health.Path); err != nil {
			return fmt.Errorf("failed to start health server: %w", err)
		}
	}

	// 启动调度循环
	s.wg.Add(1)
	go s.scheduleLoop(s.ctx)

	// 启动所有Workers
	for _, worker := range s.workers {
		if err := worker.Start(s.ctx); err != nil {
			s.logger.Error().Err(err).Str("worker_id", worker.id).Msg("failed to start worker")
			return err
		}
	}

	s.logger.Info().Msg("scheduler started")
	return nil
}

// Shutdown 优雅关闭调度器
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if !s.running.CompareAndSwap(true, false) {
		return nil
	}

	s.logger.Info().Msg("scheduler shutting down")

	// 取消context,发送停止信号
	if s.cancel != nil {
		s.cancel()
	}

	// 停止所有Workers
	g, gctx := errgroup.WithContext(ctx)
	for _, worker := range s.workers {
		w := worker // 捕获循环变量
		g.Go(func() error {
			if err := w.Stop(gctx); err != nil {
				s.logger.Error().Err(err).Str("worker_id", w.id).Msg("failed to stop worker")
			}
			return nil // 不中断其他worker的停止
		})
	}

	// 等待所有Workers停止
	done := make(chan struct{})
	go func() {
		g.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info().Msg("all workers stopped")
	case <-time.After(s.opts.Worker.ShutdownGracePeriod):
		s.logger.Warn().Msg("worker shutdown timeout")
	}

	// 等待调度循环退出
	s.wg.Wait()

	// 停止HTTP服务器
	if s.metricsServer != nil {
		s.metricsServer.Shutdown(ctx)
	}
	if s.healthChecker != nil {
		s.healthChecker.Stop(ctx)
	}

	s.logger.Info().Msg("scheduler shutdown completed")
	return nil
}

// scheduleLoop 调度循环
func (s *Scheduler) scheduleLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.opts.ScanInterval)
	defer ticker.Stop()

	s.logger.Info().Msg("schedule loop started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("schedule loop stopped: context cancelled")
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}

// scan 扫描延迟队列，移动到期任务到就绪队列
func (s *Scheduler) scan(ctx context.Context) {
	now := time.Now().Unix()

	// 移动到期任务
	moved, err := s.queue.MoveDelayedToReady(ctx, now, s.opts.BatchSize)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to move delayed tasks")
		if s.opts.CircuitBreaker.Enabled {
			s.circuitBreaker.RecordFailure()
		}
		return
	}

	if moved > 0 {
		s.logger.Debug().Int64("count", moved).Msg("moved tasks from delayed to ready queue")
		if s.opts.CircuitBreaker.Enabled {
			s.circuitBreaker.RecordSuccess()
		}
	}

	// 接管超时的Pending消息（故障恢复）
	s.reclaimPendingMessages(ctx)

	// 更新队列指标
	if s.metrics.enabled {
		s.updateQueueMetrics(ctx)
	}
}

// reclaimPendingMessages 接管超时的Pending消息（并发处理不同优先级）
func (s *Scheduler) reclaimPendingMessages(ctx context.Context) {
	// 设置超时时间：任务超时的倍数（确保任务已经处理失败或Worker崩溃）
	idleTime := s.opts.LockTimeout * idleTimeMultiplier

	// 创建带超时的 context，但保留父 context 的取消信号
	timeoutCtx, cancel := context.WithTimeout(ctx, reclaimPendingTimeout*time.Second)
	defer cancel()

	// 使用errgroup并发处理不同优先级
	g, gctx := errgroup.WithContext(timeoutCtx)
	priorities := [...]Priority{PriorityHigh, PriorityNormal, PriorityLow}

	for _, priority := range priorities {
		p := priority // 捕获循环变量
		g.Go(func() error {
			claimed, err := s.queue.ClaimStaleMessages(gctx, p, idleTime)
			if err != nil {
				s.logger.Error().Err(err).Int("priority", int(p)).Msg("failed to claim stale messages")
				return err // 返回错误以记录
			}

			if len(claimed) > 0 {
				s.logger.Info().Int("count", len(claimed)).Int("priority", int(p)).Msg("reclaimed stale pending messages")
			}
			return nil
		})
	}

	// 等待所有优先级处理完成，记录聚合错误
	if err := g.Wait(); err != nil {
		s.logger.Warn().Err(err).Msg("some priorities failed to reclaim stale messages")
	}
}

// updateQueueMetrics 更新队列指标
func (s *Scheduler) updateQueueMetrics(ctx context.Context) {
	// 检查缓存是否过期
	now := time.Now().Unix()
	lastUpdate := s.lastStatsTime.Load()
	if now-lastUpdate < statsCacheSeconds {
		return // 使用缓存，避免频繁查询
	}

	stats, err := s.queue.GetStats(ctx)
	if err != nil {
		return
	}

	// 更新缓存
	s.statsCache.Store(stats)
	s.lastStatsTime.Store(now)

	s.metrics.RecordQueueSize("delayed", float64(stats.DelayedCount))
	s.metrics.RecordQueueSize("ready_high", float64(stats.HighCount))
	s.metrics.RecordQueueSize("ready_normal", float64(stats.NormalCount))
	s.metrics.RecordQueueSize("ready_low", float64(stats.LowCount))
	s.metrics.RecordQueueSize("running", float64(stats.RunningCount))

	// 更新死信队列指标
	if dlqCount, err := s.dlq.Count(ctx); err == nil {
		s.metrics.RecordDeadLetterCount(float64(dlqCount))
	}
}

// Submit 提交泛型任务
func Submit[T any](s *Scheduler, ctx context.Context, taskType string, payload T, opts ...TaskOption) (string, error) {
	return SubmitWithSerializer(s, ctx, taskType, payload, s.registry.serializer, opts...)
}

// SubmitWithSerializer 使用指定序列化器提交泛型任务
func SubmitWithSerializer[T any](s *Scheduler, ctx context.Context, taskType string, payload T, serializer Serializer, opts ...TaskOption) (string, error) {
	// 序列化 payload
	payloadBytes, err := serializer.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	// 创建任务
	task := &Task{
		Type:    taskType,
		Payload: payloadBytes,
	}

	// 应用选项
	for _, opt := range opts {
		opt(task)
	}

	return s.submitTask(ctx, task)
}

// submitTask 内部提交任务方法
func (s *Scheduler) submitTask(ctx context.Context, task *Task) (string, error) {
	// 验证任务
	if err := task.Validate(); err != nil {
		return "", err
	}

	// 处理 Cron 任务
	if task.Cron != "" {
		// 验证 Cron 表达式
		if err := s.cronParser.Validate(task.Cron); err != nil {
			return "", fmt.Errorf("invalid cron expression: %w", err)
		}

		// 如果没有设置调度时间，计算首次执行时间
		if task.ScheduleAt.IsZero() {
			nextTime, err := s.cronParser.Next(task.Cron, time.Now())
			if err != nil {
				return "", fmt.Errorf("failed to calculate next execution time: %w", err)
			}
			task.ScheduleAt = nextTime
		}
	}

	// 生成任务ID
	if task.ID == "" {
		task.ID = uuid.New().String()
	}

	// 设置默认计划时间
	if task.ScheduleAt.IsZero() {
		task.ScheduleAt = time.Now()
	}

	// 限流检查
	if s.opts.RateLimit.Enabled && !s.rateLimiter.Allow() {
		s.metrics.RecordRateLimitRejected()
		return "", ErrRateLimitExceeded
	}

	// 熔断检查
	if s.opts.CircuitBreaker.Enabled && !s.circuitBreaker.Allow() {
		return "", ErrCircuitBreakerOpen
	}

	// 去重检查
	if task.DeduplicationKey != "" {
		duplicate, existingTaskID, err := s.dedup.Check(ctx, task.DeduplicationKey)
		if err != nil {
			return "", fmt.Errorf("deduplication check failed: %w", err)
		}
		if duplicate {
			s.logger.Debug().Str("task_id", existingTaskID).Str("dedup_key", task.DeduplicationKey).Msg("task duplicate")
			return existingTaskID, ErrTaskDuplicate
		}

		// 设置去重记录
		ttl := task.DeduplicationTTL
		if ttl == 0 {
			ttl = s.opts.DedupDefaultTTL
		}
		if err := s.dedup.Set(ctx, task.DeduplicationKey, task.ID, ttl); err != nil {
			return "", fmt.Errorf("failed to set deduplication record: %w", err)
		}
	}

	// 创建任务信息
	taskInfo := task.ToTaskInfo()

	// 使用Pipeline批量操作：保存任务元数据 + 添加到延迟队列
	pipe := s.client.Pipeline()

	// 保存任务元数据
	taskKey := s.buildTaskKey(task.ID)
	m := s.getMapFromPool()
	s.taskInfoToMap(taskInfo, m)
	pipe.HSet(ctx, taskKey, m)
	s.returnMapToPool(m)

	// 添加到延迟队列
	score := float64(task.ScheduleAt.Unix())
	delayedKey := s.opts.Namespace + ":delayed"
	pipe.ZAdd(ctx, delayedKey, redis.Z{Score: score, Member: task.ID})

	// 执行Pipeline
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("failed to submit task: %w", err)
	}

	// 记录指标
	if s.metrics.enabled {
		s.metrics.RecordTaskSubmitted(task.Type, task.Priority)
	}

	s.logger.Info().
		Str("task_id", task.ID).
		Str("type", task.Type).
		Int("priority", int(task.Priority)).
		Time("schedule_at", task.ScheduleAt).
		Msg("task submitted")

	return task.ID, nil
}

// BatchSubmit 批量提交任务
func BatchSubmit[T any](s *Scheduler, ctx context.Context, taskType string, payloads []T, opts ...TaskOption) ([]string, error) {
	return BatchSubmitWithSerializer(s, ctx, taskType, payloads, s.registry.serializer, opts...)
}

// BatchSubmitWithSerializer 使用指定序列化器批量提交任务
func BatchSubmitWithSerializer[T any](s *Scheduler, ctx context.Context, taskType string, payloads []T, serializer Serializer, opts ...TaskOption) ([]string, error) {
	if len(payloads) == 0 {
		return nil, nil
	}

	// 准备任务
	tasks := make([]*Task, 0, len(payloads))
	for _, payload := range payloads {
		// 限流检查
		if s.opts.RateLimit.Enabled && !s.rateLimiter.Allow() {
			s.metrics.RecordRateLimitRejected()
			continue
		}

		// 序列化 payload
		payloadBytes, err := serializer.Marshal(payload)
		if err != nil {
			s.logger.Error().Err(err).Str("task_type", taskType).Msg("failed to marshal payload in batch")
			continue
		}

		// 创建任务
		task := &Task{
			Type:    taskType,
			Payload: payloadBytes,
		}

		// 应用选项
		for _, opt := range opts {
			opt(task)
		}

		// 验证任务
		if err := task.Validate(); err != nil {
			s.logger.Error().Err(err).Str("task_type", taskType).Msg("invalid task in batch")
			continue
		}

		// 生成任务ID
		if task.ID == "" {
			task.ID = uuid.New().String()
		}

		// 设置默认计划时间
		if task.ScheduleAt.IsZero() {
			task.ScheduleAt = time.Now()
		}

		tasks = append(tasks, task)
	}

	if len(tasks) == 0 {
		return nil, nil
	}

	// 熔断检查
	if s.opts.CircuitBreaker.Enabled && !s.circuitBreaker.Allow() {
		return nil, ErrCircuitBreakerOpen
	}

	// 使用 Pipeline 批量提交
	pipe := s.client.Pipeline()
	delayedKey := s.opts.Namespace + ":delayed"
	taskIDs := make([]string, 0, len(tasks))

	for _, task := range tasks {
		// 去重检查
		if task.DeduplicationKey != "" {
			duplicate, existingTaskID, err := s.dedup.Check(ctx, task.DeduplicationKey)
			if err != nil {
				s.logger.Error().Err(err).Str("task_id", task.ID).Msg("deduplication check failed in batch")
				continue
			}
			if duplicate {
				s.logger.Debug().Str("task_id", existingTaskID).Str("dedup_key", task.DeduplicationKey).Msg("task duplicate in batch")
				continue
			}

			// 设置去重记录
			ttl := task.DeduplicationTTL
			if ttl == 0 {
				ttl = s.opts.DedupDefaultTTL
			}
			if err := s.dedup.Set(ctx, task.DeduplicationKey, task.ID, ttl); err != nil {
				s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to set deduplication record in batch")
				continue
			}
		}

		// 创建任务信息
		taskInfo := task.ToTaskInfo()

		// 保存任务元数据
		taskKey := s.buildTaskKey(task.ID)
		m := s.getMapFromPool()
		s.taskInfoToMap(taskInfo, m)
		pipe.HSet(ctx, taskKey, m)
		s.returnMapToPool(m)

		// 添加到延迟队列
		score := float64(task.ScheduleAt.Unix())
		pipe.ZAdd(ctx, delayedKey, redis.Z{Score: score, Member: task.ID})

		taskIDs = append(taskIDs, task.ID)

		// 记录指标
		if s.metrics.enabled {
			s.metrics.RecordTaskSubmitted(task.Type, task.Priority)
		}
	}

	// 执行Pipeline
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to batch submit tasks: %w", err)
	}

	s.logger.Info().Int("count", len(taskIDs)).Int("total", len(payloads)).Msg("tasks batch submitted")
	return taskIDs, nil
}

// CancelTask 取消任务
func (s *Scheduler) CancelTask(ctx context.Context, taskID string) error {
	// 获取任务信息
	taskInfo, err := s.GetTaskInfo(ctx, taskID)
	if err != nil {
		return err
	}

	// 只能取消Pending和Ready状态的任务
	if taskInfo.Status != StatusPending && taskInfo.Status != StatusReady {
		return fmt.Errorf("cannot cancel task in status: %s", taskInfo.Status)
	}

	// 从队列移除
	if err := s.queue.RemoveDelayed(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to remove task from delayed queue")
	}
	if err := s.queue.RemoveReady(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to remove task from ready queue")
	}

	// 更新状态
	taskInfo.Status = StatusCancelled
	now := time.Now()
	taskInfo.FinishTime = &now

	if err := s.saveTaskInfo(ctx, taskInfo); err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	s.logger.Info().Str("task_id", taskID).Msg("task cancelled")
	return nil
}

// buildTaskKey 构建任务key
func (s *Scheduler) buildTaskKey(taskID string) string {
	return s.opts.Namespace + ":task:" + taskID
}

// GetTaskInfo 获取任务信息
func (s *Scheduler) GetTaskInfo(ctx context.Context, taskID string) (*TaskInfo, error) {
	taskKey := s.buildTaskKey(taskID)

	result, err := s.client.HGetAll(ctx, taskKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get task info: %w", err)
	}

	if len(result) == 0 {
		return nil, ErrTaskNotFound
	}

	// 直接创建新对象
	taskInfo := &TaskInfo{}
	if err := taskInfo.FromMap(result); err != nil {
		return nil, fmt.Errorf("failed to parse task info: %w", err)
	}

	return taskInfo, nil
}

// saveTaskInfo 保存任务信息
func (s *Scheduler) saveTaskInfo(ctx context.Context, taskInfo *TaskInfo) error {
	taskKey := s.buildTaskKey(taskInfo.ID)

	// 使用对象池获取map
	m := s.getMapFromPool()
	defer s.returnMapToPool(m)

	// 填充map
	s.taskInfoToMap(taskInfo, m)

	err := s.client.HSet(ctx, taskKey, m).Err()
	if err != nil {
		return fmt.Errorf("failed to save task info: %w", err)
	}

	return nil
}

// getMapFromPool 从对象池获取map
func (s *Scheduler) getMapFromPool() map[string]any {
	m := s.mapPool.Get().(map[string]any)
	// 清空map（防止脏数据）
	for k := range m {
		delete(m, k)
	}
	return m
}

// returnMapToPool 归还map到对象池
func (s *Scheduler) returnMapToPool(m map[string]any) {
	// 如果map太大，不放回池中
	if len(m) > mapPoolMaxSize {
		return
	}
	s.mapPool.Put(m)
}

// taskInfoToMap 将TaskInfo转换为Map
func (s *Scheduler) taskInfoToMap(t *TaskInfo, m map[string]any) {
	m["id"] = t.ID
	m["type"] = t.Type
	m["priority"] = int(t.Priority)
	m["payload"] = string(t.Payload)
	m["schedule_at"] = t.ScheduleAt.Unix()
	m["cron"] = t.Cron
	m["max_retry"] = t.MaxRetry
	m["timeout"] = t.Timeout.Seconds()
	m["deduplication_key"] = t.DeduplicationKey
	m["deduplication_ttl"] = t.DeduplicationTTL.Seconds()
	m["status"] = string(t.Status)
	m["retry_count"] = t.RetryCount
	m["worker_id"] = t.WorkerID
	m["submit_time"] = t.SubmitTime.Unix()
	m["last_error"] = t.LastError

	if len(t.Tags) > 0 {
		tagsJSON, _ := json.Marshal(t.Tags)
		m["tags"] = string(tagsJSON)
	}
	if len(t.Context) > 0 {
		ctxJSON, _ := json.Marshal(t.Context)
		m["context"] = string(ctxJSON)
	}
	if t.StartTime != nil {
		m["start_time"] = t.StartTime.Unix()
	}
	if t.FinishTime != nil {
		m["finish_time"] = t.FinishTime.Unix()
	}
	if t.ExecutionTime != nil {
		m["execution_time"] = t.ExecutionTime.Seconds()
	}
}

// deleteTaskInfo 删除任务信息
func (s *Scheduler) deleteTaskInfo(ctx context.Context, taskID string) error {
	taskKey := s.buildTaskKey(taskID)
	err := s.client.Del(ctx, taskKey).Err()
	if err != nil {
		return fmt.Errorf("failed to delete task info: %w", err)
	}

	return nil
}

// scheduleNextCron 调度Cron任务的下次执行
func (s *Scheduler) scheduleNextCron(ctx context.Context, taskInfo *TaskInfo) {
	nextTime, err := s.cronParser.Next(taskInfo.Cron, time.Now())
	if err != nil {
		s.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to calculate next cron time")
		return
	}

	// 创建新的任务实例
	newTask := &Task{
		ID:         uuid.New().String(),
		Type:       taskInfo.Type,
		Priority:   taskInfo.Priority,
		Payload:    taskInfo.Payload,
		ScheduleAt: nextTime,
		Cron:       taskInfo.Cron,
		MaxRetry:   taskInfo.MaxRetry,
		Timeout:    taskInfo.Timeout,
		Tags:       taskInfo.Tags,
		Context:    taskInfo.Context,
	}

	if _, err := s.submitTask(ctx, newTask); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskInfo.ID).Msg("failed to schedule next cron task")
	} else {
		s.logger.Info().
			Str("original_task_id", taskInfo.ID).
			Str("new_task_id", newTask.ID).
			Time("next_run", nextTime).
			Msg("next cron task scheduled")
	}
}

// GetQueueStats 获取队列统计信息
func (s *Scheduler) GetQueueStats(ctx context.Context) (*QueueStats, error) {
	stats, err := s.queue.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	// 添加Worker数量
	if count, err := s.getActiveWorkerCount(ctx); err == nil {
		stats.WorkerCount = int64(count)
	}

	// 添加DLQ数量
	if dlqCount, err := s.dlq.Count(ctx); err == nil {
		stats.DLQCount = dlqCount
	}

	return stats, nil
}

// startMetricsServer 启动Prometheus指标服务器
func (s *Scheduler) startMetricsServer() error {
	mux := http.NewServeMux()
	mux.Handle(s.opts.Metrics.Path, promhttp.Handler())

	s.metricsServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.opts.Metrics.Port),
		Handler: mux,
	}

	go func() {
		s.logger.Info().Int("port", s.opts.Metrics.Port).Str("path", s.opts.Metrics.Path).Msg("metrics server starting")
		if err := s.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("metrics server error")
		}
	}()

	return nil
}

// getActiveWorkerCount 获取活跃 worker 数量（基于 SCAN，利用 Redis TTL 自动过期）
func (s *Scheduler) getActiveWorkerCount(ctx context.Context) (int, error) {
	pattern := fmt.Sprintf("%s:worker:*", s.opts.Namespace)
	var count int

	iter := s.client.Scan(ctx, 0, pattern, workerScanBatchSize).Iterator()
	for iter.Next(ctx) {
		count++
	}
	if err := iter.Err(); err != nil {
		return 0, err
	}

	return count, nil
}
