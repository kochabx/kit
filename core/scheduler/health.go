package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthChecker 健康检查器
type HealthChecker struct {
	scheduler *Scheduler
	server    *http.Server
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(scheduler *Scheduler) *HealthChecker {
	return &HealthChecker{
		scheduler: scheduler,
	}
}

// Start 启动健康检查HTTP服务
func (h *HealthChecker) Start(port int, path string) error {
	mux := http.NewServeMux()
	mux.HandleFunc(path, h.handleHealth)

	h.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.scheduler.logger.Error().Err(err).Msg("health check server error")
		}
	}()

	return nil
}

// Stop 停止健康检查服务
func (h *HealthChecker) Stop(ctx context.Context) error {
	if h.server != nil {
		return h.server.Shutdown(ctx)
	}
	return nil
}

// handleHealth 健康检查处理器
func (h *HealthChecker) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status := h.Check(ctx)

	w.Header().Set("Content-Type", "application/json")

	switch status.Status {
	case "healthy":
		w.WriteHeader(http.StatusOK)
	case "degraded":
		w.WriteHeader(http.StatusOK) // 降级状态仍返回200
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(status)
}

// HealthStatus 健康状态
type HealthStatus struct {
	Status    string                 `json:"status"`    // healthy/degraded/unhealthy
	Timestamp time.Time              `json:"timestamp"` // 检查时间
	Checks    map[string]CheckResult `json:"checks"`    // 各项检查结果
}

// CheckResult 检查结果
type CheckResult struct {
	Status  string `json:"status"`            // ok/warning/error
	Message string `json:"message,omitempty"` // 描述信息
}

// Check 执行健康检查
func (h *HealthChecker) Check(ctx context.Context) *HealthStatus {
	checks := make(map[string]CheckResult)
	overallStatus := "healthy"

	// 1. 检查Redis连接
	redisCheck := h.checkRedis(ctx)
	checks["redis"] = redisCheck
	if redisCheck.Status == "error" {
		overallStatus = "unhealthy"
	}

	// 2. 检查Worker数量
	workerCheck := h.checkWorkers(ctx)
	checks["workers"] = workerCheck
	if workerCheck.Status == "error" {
		overallStatus = "unhealthy"
	} else if workerCheck.Status == "warning" && overallStatus == "healthy" {
		overallStatus = "degraded"
	}

	// 3. 检查队列状态
	queueCheck := h.checkQueues(ctx)
	checks["queues"] = queueCheck
	if queueCheck.Status == "warning" && overallStatus == "healthy" {
		overallStatus = "degraded"
	}

	// 4. 检查死信队列
	dlqCheck := h.checkDLQ(ctx)
	checks["dlq"] = dlqCheck
	if dlqCheck.Status == "warning" && overallStatus == "healthy" {
		overallStatus = "degraded"
	}

	return &HealthStatus{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Checks:    checks,
	}
}

// checkRedis 检查Redis连接
func (h *HealthChecker) checkRedis(ctx context.Context) CheckResult {
	if err := h.scheduler.client.Ping(ctx).Err(); err != nil {
		return CheckResult{
			Status:  "error",
			Message: fmt.Sprintf("redis ping failed: %v", err),
		}
	}
	return CheckResult{
		Status:  "ok",
		Message: "redis connection healthy",
	}
}

// checkWorkers 检查Worker状态
func (h *HealthChecker) checkWorkers(ctx context.Context) CheckResult {
	count, err := h.scheduler.getActiveWorkerCount(ctx)
	if err != nil {
		return CheckResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to get worker count: %v", err),
		}
	}

	if count == 0 {
		return CheckResult{
			Status:  "error",
			Message: "no active workers",
		}
	}

	expectedWorkers := h.scheduler.opts.Worker.Count
	if count < expectedWorkers {
		return CheckResult{
			Status:  "warning",
			Message: fmt.Sprintf("worker count (%d) below expected (%d)", count, expectedWorkers),
		}
	}

	return CheckResult{
		Status:  "ok",
		Message: fmt.Sprintf("%d workers active", count),
	}
}

// checkQueues 检查队列状态
func (h *HealthChecker) checkQueues(ctx context.Context) CheckResult {
	stats, err := h.scheduler.queue.GetStats(ctx)
	if err != nil {
		return CheckResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to get queue stats: %v", err),
		}
	}

	totalReady := stats.HighCount + stats.NormalCount + stats.LowCount
	totalQueued := stats.DelayedCount + totalReady

	// 如果队列积压超过10000个任务，发出警告
	if totalQueued > 10000 {
		return CheckResult{
			Status:  "warning",
			Message: fmt.Sprintf("queue backlog: %d tasks", totalQueued),
		}
	}

	return CheckResult{
		Status:  "ok",
		Message: fmt.Sprintf("queued: %d, running: %d", totalQueued, stats.RunningCount),
	}
}

// checkDLQ 检查死信队列
func (h *HealthChecker) checkDLQ(ctx context.Context) CheckResult {
	count, err := h.scheduler.dlq.Count(ctx)
	if err != nil {
		return CheckResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to get DLQ count: %v", err),
		}
	}

	if count > 100 {
		return CheckResult{
			Status:  "warning",
			Message: fmt.Sprintf("DLQ has %d tasks", count),
		}
	}

	return CheckResult{
		Status:  "ok",
		Message: fmt.Sprintf("DLQ: %d tasks", count),
	}
}
