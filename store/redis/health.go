package redis

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// HealthChecker 健康检查器
type HealthChecker struct {
	client   redis.UniversalClient
	interval time.Duration
	logger   *log.Logger

	// 状态
	mu         sync.RWMutex
	lastStatus *HealthStatus
	running    atomic.Bool

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// HealthStatus 健康状态
type HealthStatus struct {
	Healthy      bool             // 是否健康
	LastCheck    time.Time        // 最后检查时间
	Latency      time.Duration    // 延迟
	PoolStats    *redis.PoolStats // 连接池统计
	ErrorMessage string           // 错误信息
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(client redis.UniversalClient, interval time.Duration, logger *log.Logger) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())

	return &HealthChecker{
		client:   client,
		interval: interval,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
}

// Start 启动健康检查
func (hc *HealthChecker) Start() error {
	if hc.running.Load() {
		return nil
	}

	hc.running.Store(true)

	// 立即执行一次检查
	hc.check()

	// 启动定期检查
	go hc.run()

	if hc.logger != nil {
		hc.logger.Info().Dur("interval", hc.interval).Msg("health checker started")
	}

	return nil
}

// Stop 停止健康检查
func (hc *HealthChecker) Stop() error {
	if !hc.running.Load() {
		return nil
	}

	hc.running.Store(false)
	hc.cancel()

	// 等待检查协程退出
	<-hc.done

	if hc.logger != nil {
		hc.logger.Info().Msg("health checker stopped")
	}

	return nil
}

// GetStatus 获取当前健康状态
func (hc *HealthChecker) GetStatus() *HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if hc.lastStatus == nil {
		return &HealthStatus{
			Healthy:      false,
			LastCheck:    time.Time{},
			ErrorMessage: "not checked yet",
		}
	}

	// 返回副本
	status := *hc.lastStatus
	return &status
}

// run 定期执行健康检查
func (hc *HealthChecker) run() {
	defer close(hc.done)

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			hc.check()
		}
	}
}

// check 执行一次健康检查
func (hc *HealthChecker) check() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status := &HealthStatus{
		LastCheck: time.Now(),
	}

	// 执行 PING 命令
	start := time.Now()
	err := hc.client.Ping(ctx).Err()
	status.Latency = time.Since(start)

	if err != nil {
		status.Healthy = false
		status.ErrorMessage = err.Error()

		if hc.logger != nil {
			hc.logger.Error().Dur("latency", status.Latency).Err(err).Msg("health check failed")
		}
	} else {
		status.Healthy = true

		// 获取连接池统计
		status.PoolStats = hc.client.PoolStats()

		if hc.logger != nil {
			hc.logger.Debug().Dur("latency", status.Latency).Uint32("hits", status.PoolStats.Hits).Uint32("misses", status.PoolStats.Misses).Uint32("timeouts", status.PoolStats.Timeouts).Uint32("total_conns", status.PoolStats.TotalConns).Uint32("idle_conns", status.PoolStats.IdleConns).Uint32("stale_conns", status.PoolStats.StaleConns).Msg("health check success")
		}
	}

	// 更新状态
	hc.mu.Lock()
	hc.lastStatus = status
	hc.mu.Unlock()
}

// IsHealthy 检查是否健康
func (hc *HealthChecker) IsHealthy() bool {
	status := hc.GetStatus()
	return status.Healthy
}

// WaitForHealthy 等待健康状态
// 返回 true 表示已健康，返回 false 表示超时
func (hc *HealthChecker) WaitForHealthy(ctx context.Context, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if hc.IsHealthy() {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}
