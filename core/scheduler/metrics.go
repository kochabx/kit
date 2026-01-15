package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics Prometheus指标收集器
type Metrics struct {
	enabled bool

	// 任务提交指标
	TaskSubmitted *prometheus.CounterVec // 任务提交总数

	// 任务执行指标
	TaskExecuted *prometheus.CounterVec   // 任务执行总数（按状态：success/failed）
	TaskDuration *prometheus.HistogramVec // 任务执行时长

	// 队列指标
	QueueSize *prometheus.GaugeVec // 队列长度

	// Worker指标
	WorkerCount      prometheus.Gauge       // Worker数量
	WorkerTaskCount  *prometheus.CounterVec // Worker执行任务数
	WorkerActiveTime *prometheus.GaugeVec   // Worker活跃时间

	// 锁指标
	LockWaitDuration *prometheus.HistogramVec // 锁等待时间
	LockAcquired     *prometheus.CounterVec   // 锁获取结果（success/failed）

	// 重试指标
	TaskRetry *prometheus.CounterVec // 任务重试次数

	// 死信队列指标
	DeadLetterCount prometheus.Gauge // 死信队列任务数

	// 限流指标
	RateLimitRejected prometheus.Counter // 限流拒绝次数

	// 熔断器指标
	CircuitBreakerState *prometheus.GaugeVec // 熔断器状态（0=closed, 1=open, 2=half-open）
}

// NewMetrics 创建指标收集器
func NewMetrics(namespace string, enabled bool) *Metrics {
	if !enabled {
		return &Metrics{enabled: false}
	}

	m := &Metrics{
		enabled: true,

		TaskSubmitted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "task_submitted_total",
				Help:      "Total number of submitted tasks",
			},
			[]string{"type", "priority"},
		),

		TaskExecuted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "task_executed_total",
				Help:      "Total number of executed tasks",
			},
			[]string{"type", "status"},
		),

		TaskDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "task_duration_seconds",
				Help:      "Task execution duration in seconds",
				Buckets:   []float64{0.001, 0.01, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300},
			},
			[]string{"type"},
		),

		QueueSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "queue_size",
				Help:      "Number of tasks in queue",
			},
			[]string{"queue"},
		),

		WorkerCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "worker_count",
				Help:      "Number of active workers",
			},
		),

		WorkerTaskCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "worker_task_total",
				Help:      "Total number of tasks processed by worker",
			},
			[]string{"worker_id"},
		),

		WorkerActiveTime: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "worker_active_seconds",
				Help:      "Worker active time in seconds",
			},
			[]string{"worker_id"},
		),

		LockWaitDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "lock_wait_duration_seconds",
				Help:      "Lock wait duration in seconds",
				Buckets:   []float64{0.001, 0.01, 0.1, 0.5, 1, 5},
			},
			[]string{"type"},
		),

		LockAcquired: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "lock_acquired_total",
				Help:      "Total number of lock acquisition attempts",
			},
			[]string{"type", "result"},
		),

		TaskRetry: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "task_retry_total",
				Help:      "Total number of task retries",
			},
			[]string{"type", "retry_count"},
		),

		DeadLetterCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "dead_letter_queue_size",
				Help:      "Number of tasks in dead letter queue",
			},
		),

		RateLimitRejected: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "rate_limit_rejected_total",
				Help:      "Total number of rejected requests due to rate limiting",
			},
		),

		CircuitBreakerState: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "circuit_breaker_state",
				Help:      "Circuit breaker state (0=closed, 1=open, 2=half-open)",
			},
			[]string{"name"},
		),
	}

	return m
}

// RecordTaskSubmitted 记录任务提交
func (m *Metrics) RecordTaskSubmitted(taskType string, priority Priority) {
	if !m.enabled {
		return
	}
	// 使用预计算的字符串避免重复转换
	var priorityStr string
	if priority >= PriorityHigh {
		priorityStr = "high"
	} else if priority >= PriorityNormal {
		priorityStr = "normal"
	} else {
		priorityStr = "low"
	}
	m.TaskSubmitted.WithLabelValues(taskType, priorityStr).Inc()
}

// RecordTaskExecuted 记录任务执行
func (m *Metrics) RecordTaskExecuted(taskType string, status TaskStatus, duration float64) {
	if !m.enabled {
		return
	}
	m.TaskExecuted.WithLabelValues(taskType, string(status)).Inc()
	m.TaskDuration.WithLabelValues(taskType).Observe(duration)
}

// RecordQueueSize 记录队列大小
func (m *Metrics) RecordQueueSize(queue string, size float64) {
	if !m.enabled {
		return
	}
	m.QueueSize.WithLabelValues(queue).Set(size)
}

// RecordWorkerCount 记录Worker数量
func (m *Metrics) RecordWorkerCount(count float64) {
	if !m.enabled {
		return
	}
	m.WorkerCount.Set(count)
}

// RecordWorkerTask 记录Worker执行任务
func (m *Metrics) RecordWorkerTask(workerID string) {
	if !m.enabled {
		return
	}
	m.WorkerTaskCount.WithLabelValues(workerID).Inc()
}

// RecordWorkerActiveTime 记录Worker活跃时间
func (m *Metrics) RecordWorkerActiveTime(workerID string, seconds float64) {
	if !m.enabled {
		return
	}
	m.WorkerActiveTime.WithLabelValues(workerID).Set(seconds)
}

// RecordLockWait 记录锁等待时间
func (m *Metrics) RecordLockWait(lockType string, duration float64) {
	if !m.enabled {
		return
	}
	m.LockWaitDuration.WithLabelValues(lockType).Observe(duration)
}

// RecordLockAcquired 记录锁获取
func (m *Metrics) RecordLockAcquired(lockType string, success bool) {
	if !m.enabled {
		return
	}
	// 使用静态字符串避免分配
	result := "failed"
	if success {
		result = "success"
	}
	m.LockAcquired.WithLabelValues(lockType, result).Inc()
}

// RecordTaskRetry 记录任务重试
func (m *Metrics) RecordTaskRetry(taskType string, retryCount int) {
	if !m.enabled {
		return
	}
	// 使用查找表避免类型转换
	var retryStr string
	switch retryCount {
	case 0:
		retryStr = "0"
	case 1:
		retryStr = "1"
	case 2:
		retryStr = "2"
	case 3:
		retryStr = "3"
	case 4:
		retryStr = "4"
	case 5:
		retryStr = "5"
	default:
		retryStr = "6+"
	}
	m.TaskRetry.WithLabelValues(taskType, retryStr).Inc()
}

// RecordDeadLetterCount 记录死信队列任务数
func (m *Metrics) RecordDeadLetterCount(count float64) {
	if !m.enabled {
		return
	}
	m.DeadLetterCount.Set(count)
}

// RecordRateLimitRejected 记录限流拒绝
func (m *Metrics) RecordRateLimitRejected() {
	if !m.enabled {
		return
	}
	m.RateLimitRejected.Inc()
}

// RecordCircuitBreakerState 记录熔断器状态
func (m *Metrics) RecordCircuitBreakerState(name string, state CircuitState) {
	if !m.enabled {
		return
	}
	m.CircuitBreakerState.WithLabelValues(name).Set(float64(state))
}
