package scheduler

import (
	"encoding/json"
	"time"
)

// TaskType 任务类型
type TaskType string

const (
	TaskTypeImmediate TaskType = "immediate" // 立即执行
	TaskTypeDelayed   TaskType = "delayed"   // 延迟执行
	TaskTypeCron      TaskType = "cron"      // 定时执行（周期性）
	TaskTypeOnce      TaskType = "once"      // 仅执行一次
)

// Priority 任务优先级
type Priority int

const (
	PriorityLow    Priority = 1  // 低优先级
	PriorityNormal Priority = 5  // 普通优先级
	PriorityHigh   Priority = 10 // 高优先级
)

// TaskStatus 任务状态
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"   // 等待中（延迟队列）
	StatusReady     TaskStatus = "ready"     // 就绪（就绪队列）
	StatusRunning   TaskStatus = "running"   // 执行中
	StatusSuccess   TaskStatus = "success"   // 成功
	StatusFailed    TaskStatus = "failed"    // 失败
	StatusCancelled TaskStatus = "cancelled" // 已取消
	StatusDead      TaskStatus = "dead"      // 死信
)

// Task 任务定义
type Task struct {
	ID               string            `json:"id"`                          // 任务ID（UUID）
	Type             string            `json:"type"`                        // 任务类型（用于路由到对应的Handler）
	Priority         Priority          `json:"priority"`                    // 优先级
	Payload          []byte            `json:"payload"`                     // 任务数据
	ScheduleAt       time.Time         `json:"schedule_at"`                 // 计划执行时间
	Cron             string            `json:"cron,omitempty"`              // Cron表达式
	MaxRetry         int               `json:"max_retry"`                   // 最大重试次数
	Timeout          time.Duration     `json:"timeout"`                     // 超时时间
	DeduplicationKey string            `json:"deduplication_key,omitempty"` // 去重键
	DeduplicationTTL time.Duration     `json:"deduplication_ttl,omitempty"` // 去重窗口
	Tags             map[string]string `json:"tags,omitempty"`              // 标签
	Context          map[string]any    `json:"context,omitempty"`           // 上下文数据
}

// TaskInfo 任务详细信息（包含执行状态）
type TaskInfo struct {
	Task
	Status        TaskStatus     `json:"status"`                   // 任务状态
	RetryCount    int            `json:"retry_count"`              // 当前重试次数
	WorkerID      string         `json:"worker_id,omitempty"`      // 执行Worker ID
	SubmitTime    time.Time      `json:"submit_time"`              // 提交时间
	StartTime     *time.Time     `json:"start_time,omitempty"`     // 开始执行时间
	FinishTime    *time.Time     `json:"finish_time,omitempty"`    // 完成时间
	LastError     string         `json:"last_error,omitempty"`     // 最后错误信息
	ExecutionTime *time.Duration `json:"execution_time,omitempty"` // 执行耗时
}

// Reset 重置TaskInfo（用于对象池）
func (t *TaskInfo) Reset() {
	t.ID = ""
	t.Type = ""
	t.Priority = 0
	t.Payload = nil
	t.ScheduleAt = time.Time{}
	t.Cron = ""
	t.MaxRetry = 0
	t.Timeout = 0
	t.DeduplicationKey = ""
	t.DeduplicationTTL = 0
	t.Tags = nil
	t.Context = nil
	t.Status = ""
	t.RetryCount = 0
	t.WorkerID = ""
	t.SubmitTime = time.Time{}
	t.StartTime = nil
	t.FinishTime = nil
	t.LastError = ""
	t.ExecutionTime = nil
}

// ToMap 将TaskInfo转换为Map（用于存储到Redis Hash）
func (t *TaskInfo) ToMap() map[string]any {
	m := make(map[string]any)
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

	return m
}

// FromMap 从Map加载TaskInfo（从Redis Hash读取）
func (t *TaskInfo) FromMap(m map[string]string) error {
	t.ID = m["id"]
	t.Type = m["type"]
	t.Payload = []byte(m["payload"])
	t.Cron = m["cron"]
	t.DeduplicationKey = m["deduplication_key"]
	t.Status = TaskStatus(m["status"])
	t.WorkerID = m["worker_id"]
	t.LastError = m["last_error"]

	// 解析整数
	if v := m["priority"]; v != "" {
		var priority int
		json.Unmarshal([]byte(v), &priority)
		t.Priority = Priority(priority)
	}
	if v := m["max_retry"]; v != "" {
		json.Unmarshal([]byte(v), &t.MaxRetry)
	}
	if v := m["retry_count"]; v != "" {
		json.Unmarshal([]byte(v), &t.RetryCount)
	}

	// 解析时间戳
	if v := m["schedule_at"]; v != "" {
		var ts int64
		json.Unmarshal([]byte(v), &ts)
		t.ScheduleAt = time.Unix(ts, 0)
	}
	if v := m["submit_time"]; v != "" {
		var ts int64
		json.Unmarshal([]byte(v), &ts)
		t.SubmitTime = time.Unix(ts, 0)
	}
	if v := m["start_time"]; v != "" && v != "0" {
		var ts int64
		json.Unmarshal([]byte(v), &ts)
		st := time.Unix(ts, 0)
		t.StartTime = &st
	}
	if v := m["finish_time"]; v != "" && v != "0" {
		var ts int64
		json.Unmarshal([]byte(v), &ts)
		ft := time.Unix(ts, 0)
		t.FinishTime = &ft
	}

	// 解析时长
	if v := m["timeout"]; v != "" {
		var sec float64
		json.Unmarshal([]byte(v), &sec)
		t.Timeout = time.Duration(sec * float64(time.Second))
	}
	if v := m["deduplication_ttl"]; v != "" {
		var sec float64
		json.Unmarshal([]byte(v), &sec)
		t.DeduplicationTTL = time.Duration(sec * float64(time.Second))
	}
	if v := m["execution_time"]; v != "" && v != "0" {
		var sec float64
		json.Unmarshal([]byte(v), &sec)
		dur := time.Duration(sec * float64(time.Second))
		t.ExecutionTime = &dur
	}

	// 解析JSON
	if v := m["tags"]; v != "" {
		json.Unmarshal([]byte(v), &t.Tags)
	}
	if v := m["context"]; v != "" {
		json.Unmarshal([]byte(v), &t.Context)
	}

	return nil
}

// WorkerInfo Worker信息
type WorkerInfo struct {
	ID            string    `json:"id"`
	StartTime     time.Time `json:"start_time"`
	TaskCount     int64     `json:"task_count"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// ToMap 转换为Map
func (w *WorkerInfo) ToMap() map[string]any {
	return map[string]any{
		"id":             w.ID,
		"start_time":     w.StartTime.Unix(),
		"task_count":     w.TaskCount,
		"last_heartbeat": w.LastHeartbeat.Unix(),
	}
}

// FromMap 从Map加载
func (w *WorkerInfo) FromMap(m map[string]string) error {
	w.ID = m["id"]

	if v := m["task_count"]; v != "" {
		json.Unmarshal([]byte(v), &w.TaskCount)
	}
	if v := m["start_time"]; v != "" {
		var ts int64
		json.Unmarshal([]byte(v), &ts)
		w.StartTime = time.Unix(ts, 0)
	}
	if v := m["last_heartbeat"]; v != "" {
		var ts int64
		json.Unmarshal([]byte(v), &ts)
		w.LastHeartbeat = time.Unix(ts, 0)
	}

	return nil
}

// QueueStats 队列统计信息
type QueueStats struct {
	DelayedCount int64 `json:"delayed_count"` // 延迟队列任务数
	HighCount    int64 `json:"high_count"`    // 高优先级队列任务数
	NormalCount  int64 `json:"normal_count"`  // 普通优先级队列任务数
	LowCount     int64 `json:"low_count"`     // 低优先级队列任务数
	RunningCount int64 `json:"running_count"` // 运行中任务数
	DLQCount     int64 `json:"dlq_count"`     // 死信队列任务数
	WorkerCount  int64 `json:"worker_count"`  // Worker数量
}
