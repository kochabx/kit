package redis

import (
	"context"
	"net"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// DebugHook 调试钩子（日志记录 + 慢查询检测）
type DebugHook struct {
	logger          *log.Logger
	slowQueryThresh time.Duration // 0 表示不检测慢查询
}

// NewDebugHook 创建调试 Hook
// slowQueryThresh: 慢查询阈值，0 表示不检测慢查询
func NewDebugHook(logger *log.Logger, slowQueryThresh time.Duration) *DebugHook {
	return &DebugHook{
		logger:          logger,
		slowQueryThresh: slowQueryThresh,
	}
}

func (h *DebugHook) DialHook(next redis.DialHook) redis.DialHook {
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

func (h *DebugHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		// 慢查询检测
		if h.slowQueryThresh > 0 && duration > h.slowQueryThresh {
			h.logger.Warn().Str("cmd", cmd.FullName()).Interface("args", cmd.Args()).Dur("duration", duration).Dur("threshold", h.slowQueryThresh).Msg("slow query detected")
			return err
		}

		if err != nil {
			h.logger.Warn().Str("cmd", cmd.FullName()).Interface("args", cmd.Args()).Dur("duration", duration).Err(err).Msg("redis command failed")
		} else {
			h.logger.Debug().Str("cmd", cmd.FullName()).Dur("duration", duration).Msg("redis command success")
		}
		return err
	}
}

func (h *DebugHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)

		// 慢查询检测
		if h.slowQueryThresh > 0 && duration > h.slowQueryThresh {
			cmdNames := make([]string, len(cmds))
			for i, cmd := range cmds {
				cmdNames[i] = cmd.FullName()
			}
			h.logger.Warn().Strs("commands", cmdNames).Int("count", len(cmds)).Dur("duration", duration).Dur("threshold", h.slowQueryThresh).Msg("slow pipeline detected")
			return err
		}

		if err != nil {
			h.logger.Warn().Int("commands", len(cmds)).Dur("duration", duration).Err(err).Msg("redis pipeline failed")
		} else {
			h.logger.Debug().Int("commands", len(cmds)).Dur("duration", duration).Msg("redis pipeline success")
		}
		return err
	}
}
