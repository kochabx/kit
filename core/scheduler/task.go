package scheduler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Serializer 序列化器接口
type Serializer interface {
	// Marshal 序列化对象为字节数组
	Marshal(v any) ([]byte, error)
	// Unmarshal 反序列化字节数组为对象
	Unmarshal(data []byte, v any) error
}

// JSONSerializer JSON序列化器
type JSONSerializer struct{}

// Marshal 实现 Serializer 接口
func (s *JSONSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal 实现 Serializer 接口
func (s *JSONSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// DefaultSerializer 默认序列化器
var DefaultSerializer Serializer = &JSONSerializer{}

// Handler 任务处理器泛型接口
type Handler[T any] interface {
	Handle(ctx context.Context, payload T) error
}

// HandlerFunc 泛型函数类型的 Handler
type HandlerFunc[T any] func(ctx context.Context, payload T) error

// Handle 实现 Handler 接口
func (f HandlerFunc[T]) Handle(ctx context.Context, payload T) error {
	return f(ctx, payload)
}

// NewTask 创建泛型任务
func NewTask[T any](taskType string, payload T, opts ...TaskOption) (*Task, error) {
	return NewTaskWithSerializer(taskType, payload, DefaultSerializer, opts...)
}

// NewTaskWithSerializer 使用指定序列化器创建泛型任务
func NewTaskWithSerializer[T any](taskType string, payload T, serializer Serializer, opts ...TaskOption) (*Task, error) {
	data, err := serializer.Marshal(payload)
	if err != nil {
		return nil, err
	}

	task := &Task{
		ID:         uuid.New().String(),
		Type:       taskType,
		Priority:   PriorityNormal,
		Payload:    data,
		ScheduleAt: time.Now(),
		MaxRetry:   3,
		Timeout:    5 * time.Minute,
		Tags:       make(map[string]string),
		Context:    make(map[string]any),
	}

	for _, opt := range opts {
		opt(task)
	}

	return task, nil
}

// TaskOption 任务选项
type TaskOption func(*Task)

// WithID 设置任务ID
func WithID(id string) TaskOption {
	return func(t *Task) {
		t.ID = id
	}
}

// WithPriority 设置优先级
func WithPriority(priority Priority) TaskOption {
	return func(t *Task) {
		t.Priority = priority
	}
}

// WithScheduleAt 设置计划执行时间
func WithScheduleAt(scheduleAt time.Time) TaskOption {
	return func(t *Task) {
		t.ScheduleAt = scheduleAt
	}
}

// WithDelay 设置延迟时间
func WithDelay(delay time.Duration) TaskOption {
	return func(t *Task) {
		t.ScheduleAt = time.Now().Add(delay)
	}
}

// WithCron 设置Cron表达式
func WithCron(cron string) TaskOption {
	return func(t *Task) {
		t.Cron = cron
	}
}

// WithTaskTimeout 设置超时时间
func WithTaskTimeout(timeout time.Duration) TaskOption {
	return func(t *Task) {
		t.Timeout = timeout
	}
}

// WithTaskMaxRetry 设置最大重试次数
func WithTaskMaxRetry(maxRetry int) TaskOption {
	return func(t *Task) {
		t.MaxRetry = maxRetry
	}
}

// WithTaskDeduplication 设置去重配置
func WithTaskDeduplication(key string, ttl time.Duration) TaskOption {
	return func(t *Task) {
		t.DeduplicationKey = key
		t.DeduplicationTTL = ttl
	}
}

// WithTags 设置标签
func WithTags(tags map[string]string) TaskOption {
	return func(t *Task) {
		t.Tags = tags
	}
}

// WithTag 添加单个标签
func WithTag(key, value string) TaskOption {
	return func(t *Task) {
		if t.Tags == nil {
			t.Tags = make(map[string]string)
		}
		t.Tags[key] = value
	}
}

// WithContext 设置上下文数据
func WithContext(ctx map[string]any) TaskOption {
	return func(t *Task) {
		t.Context = ctx
	}
}

// WithContextValue 添加单个上下文值
func WithContextValue(key string, value any) TaskOption {
	return func(t *Task) {
		if t.Context == nil {
			t.Context = make(map[string]any)
		}
		t.Context[key] = value
	}
}

// ToTaskInfo 将Task转换为TaskInfo
func (t *Task) ToTaskInfo() *TaskInfo {
	now := time.Now()
	return &TaskInfo{
		Task:       *t,
		Status:     StatusPending,
		RetryCount: 0,
		SubmitTime: now,
	}
}

// Validate 验证任务
func (t *Task) Validate() error {
	if t.Type == "" {
		return ErrInvalidTaskType
	}
	if t.Timeout <= 0 {
		return ErrInvalidTimeout
	}
	if t.MaxRetry < 0 {
		return ErrInvalidMaxRetry
	}
	if t.Priority < PriorityLow || t.Priority > PriorityHigh {
		return ErrInvalidPriority
	}
	return nil
}
