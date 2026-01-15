package scheduler

import (
	"context"
	"fmt"
	"sync"
)

// handlerWrapper 内部 Handler 包装器
type handlerWrapper struct {
	handle     func(ctx context.Context, payload []byte) error
	serializer Serializer
}

// Registry 任务处理器注册表
type Registry struct {
	mu         sync.RWMutex
	handlers   map[string]*handlerWrapper
	serializer Serializer
}

// NewRegistry 创建注册表
func NewRegistry() *Registry {
	return &Registry{
		handlers:   make(map[string]*handlerWrapper),
		serializer: DefaultSerializer,
	}
}

// SetSerializer 设置序列化器
func (r *Registry) SetSerializer(serializer Serializer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serializer = serializer
}

// Register 注册泛型任务处理器
func Register[T any](r *Registry, taskType string, handler Handler[T]) error {
	return RegisterWithSerializer(r, taskType, handler, nil)
}

// RegisterWithSerializer 使用指定序列化器注册泛型任务处理器
func RegisterWithSerializer[T any](r *Registry, taskType string, handler Handler[T], serializer Serializer) error {
	if taskType == "" {
		return fmt.Errorf("task type cannot be empty")
	}
	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[taskType]; exists {
		return fmt.Errorf("handler for task type %s already registered", taskType)
	}

	// 使用传入的序列化器，如果为nil则使用Registry的默认序列化器
	ser := serializer
	if ser == nil {
		ser = r.serializer
	}

	// 包装为类型擦除的 handler
	wrapper := &handlerWrapper{
		serializer: ser,
		handle: func(ctx context.Context, payload []byte) error {
			var typed T
			if err := ser.Unmarshal(payload, &typed); err != nil {
				return fmt.Errorf("failed to unmarshal payload: %w", err)
			}
			return handler.Handle(ctx, typed)
		},
	}

	r.handlers[taskType] = wrapper
	return nil
}

// Unregister 注销任务处理器
func (r *Registry) Unregister(taskType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, taskType)
}

// Get 获取任务处理器（内部使用）
func (r *Registry) Get(taskType string) (*handlerWrapper, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, exists := r.handlers[taskType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for task type: %s", taskType)
	}

	return handler, nil
}

// Has 检查是否注册了指定类型的处理器
func (r *Registry) Has(taskType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.handlers[taskType]
	return exists
}

// List 列出所有已注册的任务类型
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// Clear 清空所有注册的处理器
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = make(map[string]*handlerWrapper)
}
