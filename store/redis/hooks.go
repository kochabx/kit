package redis

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// ==================== 日志 Hook ====================

// LoggingHook 日志记录 Hook
type LoggingHook struct {
	logger *log.Logger
}

// NewLoggingHook 创建日志 Hook
func NewLoggingHook(logger *log.Logger) *LoggingHook {
	return &LoggingHook{logger: logger}
}

func (h *LoggingHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		start := time.Now()
		conn, err := next(ctx, network, addr)
		duration := time.Since(start)

		if err != nil {
			h.logger.Error().Str("network", network).Str("addr", addr).Dur("duration", duration).Err(err).Msg("redis dial failed")
		} else {
			h.logger.Debug().Str("network", network).Str("addr", addr).Dur("duration", duration).Msg("redis dial success")
		}
		return conn, err
	}
}

func (h *LoggingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		if err != nil {
			h.logger.Warn().Str("cmd", cmd.FullName()).Interface("args", cmd.Args()).Dur("duration", duration).Err(err).Msg("redis command failed")
		} else {
			h.logger.Debug().Str("cmd", cmd.FullName()).Dur("duration", duration).Msg("redis command success")
		}
		return err
	}
}

func (h *LoggingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)

		if err != nil {
			h.logger.Warn().Int("commands", len(cmds)).Dur("duration", duration).Err(err).Msg("redis pipeline failed")
		} else {
			h.logger.Debug().Int("commands", len(cmds)).Dur("duration", duration).Msg("redis pipeline success")
		}
		return err
	}
}

// ==================== Metrics Hook ====================

// MetricsHook Metrics 收集 Hook
type MetricsHook struct {
	namespace string

	// 计数器
	commandTotal   atomic.Int64
	commandSuccess atomic.Int64
	commandErrors  atomic.Int64

	// 时间统计
	totalDuration atomic.Int64 // 纳秒

	// 慢查询
	slowQueryCount  atomic.Int64
	slowQueryThresh time.Duration
}

// NewMetricsHook 创建 Metrics Hook
func NewMetricsHook(namespace string, slowQueryThresh time.Duration) *MetricsHook {
	return &MetricsHook{
		namespace:       namespace,
		slowQueryThresh: slowQueryThresh,
	}
}

func (h *MetricsHook) DialHook(next redis.DialHook) redis.DialHook {
	return next // 透传
}

func (h *MetricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		// 统计
		h.commandTotal.Add(1)
		h.totalDuration.Add(int64(duration))

		if err != nil {
			h.commandErrors.Add(1)
		} else {
			h.commandSuccess.Add(1)
		}

		// 慢查询检测
		if duration > h.slowQueryThresh {
			h.slowQueryCount.Add(1)
		}

		return err
	}
}

func (h *MetricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)

		// 统计
		h.commandTotal.Add(int64(len(cmds)))
		h.totalDuration.Add(int64(duration))

		if err != nil {
			h.commandErrors.Add(1)
		} else {
			h.commandSuccess.Add(int64(len(cmds)))
		}

		return err
	}
}

// GetMetrics 获取 Metrics 统计信息
func (h *MetricsHook) GetMetrics() *Metrics {
	total := h.commandTotal.Load()
	avgDuration := time.Duration(0)
	if total > 0 {
		avgDuration = time.Duration(h.totalDuration.Load() / total)
	}

	return &Metrics{
		CommandTotal:   total,
		CommandSuccess: h.commandSuccess.Load(),
		CommandErrors:  h.commandErrors.Load(),
		SlowQueryCount: h.slowQueryCount.Load(),
		TotalDuration:  time.Duration(h.totalDuration.Load()),
		AvgDuration:    avgDuration,
	}
}

// Metrics Metrics 统计信息
type Metrics struct {
	CommandTotal   int64
	CommandSuccess int64
	CommandErrors  int64
	SlowQueryCount int64
	TotalDuration  time.Duration
	AvgDuration    time.Duration
}

// ==================== 慢查询 Hook ====================

// SlowQueryHook 慢查询检测 Hook
type SlowQueryHook struct {
	threshold time.Duration
	logger    *log.Logger
}

// NewSlowQueryHook 创建慢查询 Hook
func NewSlowQueryHook(threshold time.Duration, logger *log.Logger) *SlowQueryHook {
	return &SlowQueryHook{
		threshold: threshold,
		logger:    logger,
	}
}

func (h *SlowQueryHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *SlowQueryHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		if duration > h.threshold {
			h.logger.Warn().Str("cmd", cmd.FullName()).Str("args", fmt.Sprintf("%v", cmd.Args())).Dur("duration", duration).Dur("threshold", h.threshold).Msg("slow query detected")
		}

		return err
	}
}

func (h *SlowQueryHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)

		if duration > h.threshold {
			cmdNames := make([]string, len(cmds))
			for i, cmd := range cmds {
				cmdNames[i] = cmd.FullName()
			}

			h.logger.Warn().Strs("commands", cmdNames).Int("count", len(cmds)).Dur("duration", duration).Dur("threshold", h.threshold).Msg("slow pipeline detected")
		}

		return err
	}
}

// ==================== Tracing Hook ====================

// TracingHook 分布式追踪 Hook
type TracingHook struct {
	serviceName string
	logger      *log.Logger
}

// NewTracingHook 创建追踪 Hook
func NewTracingHook(serviceName string, logger *log.Logger) *TracingHook {
	return &TracingHook{
		serviceName: serviceName,
		logger:      logger,
	}
}

func (h *TracingHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *TracingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// 这里可以集成 OpenTelemetry 或其他 tracing 系统
		// 示例：记录 trace 信息
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		// 简单的 trace 日志
		if h.logger != nil {
			h.logger.Debug().Str("service", h.serviceName).Str("cmd", cmd.FullName()).Dur("duration", duration).AnErr("error", err).Msg("redis trace")
		}

		return err
	}
}

func (h *TracingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
