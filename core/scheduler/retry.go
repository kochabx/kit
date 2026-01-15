package scheduler

import (
	"math"
	"math/rand"
	"time"
)

// RetryStrategy 重试策略接口
type RetryStrategy interface {
	// NextRetry 计算下次重试的延迟时间
	NextRetry(retryCount int) time.Duration
}

// ExponentialBackoff 指数退避重试策略
type ExponentialBackoff struct {
	BaseDelay  time.Duration // 基础延迟
	MaxDelay   time.Duration // 最大延迟
	Multiplier float64       // 指数乘数
	Jitter     bool          // 是否添加随机抖动
}

// NewExponentialBackoff 创建指数退避策略
func NewExponentialBackoff(baseDelay, maxDelay time.Duration, multiplier float64, jitter bool) *ExponentialBackoff {
	return &ExponentialBackoff{
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
		Multiplier: multiplier,
		Jitter:     jitter,
	}
}

// NextRetry 计算下次重试延迟
// 公式: delay = min(baseDelay * multiplier^retryCount, maxDelay)
// 如果启用抖动，则添加 ±25% 的随机变化
func (e *ExponentialBackoff) NextRetry(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}

	// 计算指数延迟
	delay := float64(e.BaseDelay) * math.Pow(e.Multiplier, float64(retryCount))

	// 限制最大延迟
	if delay > float64(e.MaxDelay) {
		delay = float64(e.MaxDelay)
	}

	// 添加随机抖动（±25%）
	if e.Jitter && delay > 0 {
		jitter := delay * 0.25 * (rand.Float64()*2 - 1)
		delay += jitter
	}

	// 确保不小于0
	if delay < 0 {
		delay = 0
	}

	return time.Duration(delay)
}

// FixedDelay 固定延迟重试策略
type FixedDelay struct {
	Delay time.Duration // 固定延迟时间
}

// NewFixedDelay 创建固定延迟策略
func NewFixedDelay(delay time.Duration) *FixedDelay {
	return &FixedDelay{
		Delay: delay,
	}
}

// NextRetry 返回固定延迟
func (f *FixedDelay) NextRetry(retryCount int) time.Duration {
	return f.Delay
}

// LinearBackoff 线性退避重试策略
type LinearBackoff struct {
	BaseDelay time.Duration // 基础延迟
	Increment time.Duration // 每次增加的延迟
	MaxDelay  time.Duration // 最大延迟
}

// NewLinearBackoff 创建线性退避策略
func NewLinearBackoff(baseDelay, increment, maxDelay time.Duration) *LinearBackoff {
	return &LinearBackoff{
		BaseDelay: baseDelay,
		Increment: increment,
		MaxDelay:  maxDelay,
	}
}

// NextRetry 计算下次重试延迟
// 公式: delay = min(baseDelay + increment * retryCount, maxDelay)
func (l *LinearBackoff) NextRetry(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}

	delay := l.BaseDelay + time.Duration(retryCount)*l.Increment

	if delay > l.MaxDelay {
		delay = l.MaxDelay
	}

	return delay
}

// NoRetry 不重试策略
type NoRetry struct{}

// NewNoRetry 创建不重试策略
func NewNoRetry() *NoRetry {
	return &NoRetry{}
}

// NextRetry 返回0（不重试）
func (n *NoRetry) NextRetry(retryCount int) time.Duration {
	return 0
}

// CustomRetry 自定义重试策略
type CustomRetry struct {
	Delays []time.Duration // 自定义延迟序列
}

// NewCustomRetry 创建自定义重试策略
func NewCustomRetry(delays []time.Duration) *CustomRetry {
	return &CustomRetry{
		Delays: delays,
	}
}

// NextRetry 根据重试次数返回对应的延迟
func (c *CustomRetry) NextRetry(retryCount int) time.Duration {
	if retryCount < 0 || len(c.Delays) == 0 {
		return 0
	}

	// 如果超出定义的延迟数，返回最后一个
	if retryCount >= len(c.Delays) {
		return c.Delays[len(c.Delays)-1]
	}

	return c.Delays[retryCount]
}
