package scheduler

import (
	"sync"
	"time"
)

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed   CircuitState = iota // 关闭状态（正常）
	StateOpen                         // 打开状态（熔断）
	StateHalfOpen                     // 半开状态（尝试恢复）
)

// String 实现Stringer接口
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	maxFailures     int           // 最大失败次数
	timeout         time.Duration // 熔断超时时间
	enabled         bool          // 是否启用
	state           CircuitState  // 当前状态
	failures        int           // 当前失败次数
	lastFailureTime time.Time     // 最后失败时间
	lastStateChange time.Time     // 最后状态变更时间
	mu              sync.RWMutex
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(enabled bool, maxFailures int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:     maxFailures,
		timeout:         timeout,
		enabled:         enabled,
		state:           StateClosed,
		failures:        0,
		lastStateChange: time.Now(),
	}
}

// Allow 检查是否允许请求通过
func (cb *CircuitBreaker) Allow() bool {
	if !cb.enabled {
		return true
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	switch cb.state {
	case StateClosed:
		// 关闭状态，允许请求
		return true
	case StateOpen:
		// 检查是否超过超时时间
		if now.Sub(cb.lastStateChange) > cb.timeout {
			// 转换为半开状态
			cb.state = StateHalfOpen
			cb.lastStateChange = now
			return true
		}
		// 仍在熔断中
		return false
	case StateHalfOpen:
		// 半开状态，允许一个请求尝试
		return true
	default:
		return false
	}
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess() {
	if !cb.enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	// 半开状态下成功，恢复为关闭状态
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
		cb.lastStateChange = time.Now()
	}
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure() {
	if !cb.enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.failures++
	cb.lastFailureTime = now

	switch cb.state {
	case StateClosed:
		// 检查是否达到最大失败次数
		if cb.failures >= cb.maxFailures {
			cb.state = StateOpen
			cb.lastStateChange = now
		}
	case StateHalfOpen:
		// 半开状态下失败，重新打开熔断器
		cb.state = StateOpen
		cb.lastStateChange = now
	}
}

// GetState 获取当前状态
func (cb *CircuitBreaker) GetState() CircuitState {
	if !cb.enabled {
		return StateClosed
	}

	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetFailures 获取当前失败次数
func (cb *CircuitBreaker) GetFailures() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.lastStateChange = time.Now()
}

// IsOpen 检查熔断器是否打开
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.GetState() == StateOpen
}

// IsClosed 检查熔断器是否关闭
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.GetState() == StateClosed
}

// IsHalfOpen 检查熔断器是否半开
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.GetState() == StateHalfOpen
}

// Enable 启用熔断器
func (cb *CircuitBreaker) Enable() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.enabled = true
}

// Disable 禁用熔断器
func (cb *CircuitBreaker) Disable() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.enabled = false
}

// IsEnabled 检查熔断器是否启用
func (cb *CircuitBreaker) IsEnabled() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.enabled
}

// Stats 获取熔断器统计信息
func (cb *CircuitBreaker) Stats() map[string]any {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]any{
		"state":             cb.state.String(),
		"failures":          cb.failures,
		"max_failures":      cb.maxFailures,
		"timeout":           cb.timeout.String(),
		"enabled":           cb.enabled,
		"last_failure_time": cb.lastFailureTime,
		"last_state_change": cb.lastStateChange,
	}
}
