package redis

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type slogHook struct {
	logger        *slog.Logger
	slowThreshold time.Duration
	now           func() time.Time
}

func newSlogHook(logger *slog.Logger, slowThreshold time.Duration) *slogHook {
	return &slogHook{
		logger:        logger,
		slowThreshold: slowThreshold,
		now:           time.Now,
	}
}

func (h *slogHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		start := h.now()
		conn, err := next(ctx, network, addr)
		d := h.now().Sub(start)

		if err != nil {
			h.logger.ErrorContext(ctx, "redis dial failed",
				slog.String("network", network),
				slog.String("addr", addr),
				slog.Duration("duration", d),
				slog.Any("err", err),
			)
		} else if h.logger.Enabled(ctx, slog.LevelDebug) {
			h.logger.DebugContext(ctx, "redis dial ok",
				slog.String("network", network),
				slog.String("addr", addr),
				slog.Duration("duration", d),
			)
		}
		return conn, err
	}
}

func (h *slogHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		start := h.now()
		err := next(ctx, cmd)
		d := h.now().Sub(start)
		slow := h.slowThreshold > 0 && d >= h.slowThreshold

		switch {
		case errors.Is(err, goredis.Nil):
			// Cache misses are expected results, never warn or error.
			if h.logger.Enabled(ctx, slog.LevelDebug) {
				h.logger.DebugContext(ctx, "redis cache miss",
					slog.String("cmd", cmd.FullName()),
					slog.Duration("duration", d),
				)
			}
		case err != nil:
			// Always report command errors accurately even when latency exceeds slowThreshold.
			h.logger.ErrorContext(ctx, "redis command failed",
				slog.String("cmd", cmd.FullName()),
				slog.Duration("duration", d),
				slog.Bool("slow", slow),
				slog.Any("err", err),
			)
		case slow:
			h.logger.WarnContext(ctx, "slow redis command",
				slog.String("cmd", cmd.FullName()),
				slog.Duration("duration", d),
				slog.Duration("threshold", h.slowThreshold),
			)
		case h.logger.Enabled(ctx, slog.LevelDebug):
			h.logger.DebugContext(ctx, "redis command ok",
				slog.String("cmd", cmd.FullName()),
				slog.Duration("duration", d),
			)
		}
		return err
	}
}

func (h *slogHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		start := h.now()
		err := next(ctx, cmds)
		d := h.now().Sub(start)
		slow := h.slowThreshold > 0 && d >= h.slowThreshold

		switch {
		case errors.Is(err, goredis.Nil):
			if h.logger.Enabled(ctx, slog.LevelDebug) {
				h.logger.DebugContext(ctx, "redis pipeline cache miss",
					slog.Int("count", len(cmds)),
					slog.Duration("duration", d),
				)
			}
		case err != nil:
			h.logger.ErrorContext(ctx, "redis pipeline failed",
				slog.Int("count", len(cmds)),
				slog.Duration("duration", d),
				slog.Bool("slow", slow),
				slog.Any("err", err),
			)
		case slow:
			cmdNames := make([]string, len(cmds))
			for i, cmd := range cmds {
				cmdNames[i] = cmd.FullName()
			}
			h.logger.WarnContext(ctx, "slow redis pipeline",
				slog.Any("commands", cmdNames),
				slog.Int("count", len(cmds)),
				slog.Duration("duration", d),
				slog.Duration("threshold", h.slowThreshold),
			)
		case h.logger.Enabled(ctx, slog.LevelDebug):
			h.logger.DebugContext(ctx, "redis pipeline ok",
				slog.Int("count", len(cmds)),
				slog.Duration("duration", d),
			)
		}
		return err
	}
}
