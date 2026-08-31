package redis

import (
	"context"
	"net"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

type debugHook struct {
	logger             *log.Logger
	slowQueryThreshold time.Duration
}

func newDebugHook(logger *log.Logger, slowQueryThreshold time.Duration) *debugHook {
	return &debugHook{
		logger:             logger,
		slowQueryThreshold: slowQueryThreshold,
	}
}

func (h *debugHook) DialHook(next redis.DialHook) redis.DialHook {
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

func (h *debugHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start)

		if h.slowQueryThreshold > 0 && duration > h.slowQueryThreshold {
			h.logger.Warn().Str("cmd", cmd.FullName()).Dur("duration", duration).Dur("threshold", h.slowQueryThreshold).Msg("slow redis command")
			return err
		}

		if err != nil {
			h.logger.Warn().Str("cmd", cmd.FullName()).Dur("duration", duration).Err(err).Msg("redis command failed")
		} else {
			h.logger.Debug().Str("cmd", cmd.FullName()).Dur("duration", duration).Msg("redis command success")
		}
		return err
	}
}

func (h *debugHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)

		if h.slowQueryThreshold > 0 && duration > h.slowQueryThreshold {
			cmdNames := make([]string, len(cmds))
			for i, cmd := range cmds {
				cmdNames[i] = cmd.FullName()
			}
			h.logger.Warn().Strs("commands", cmdNames).Int("count", len(cmds)).Dur("duration", duration).Dur("threshold", h.slowQueryThreshold).Msg("slow redis pipeline")
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
