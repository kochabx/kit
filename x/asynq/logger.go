package asynq

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/hibiken/asynq"
)

type slogAdapter struct {
	logger *slog.Logger
}

var _ asynq.Logger = (*slogAdapter)(nil)

func newSlogAdapter(logger *slog.Logger) *slogAdapter {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &slogAdapter{logger: logger}
}

func (a *slogAdapter) Debug(args ...any) {
	a.logger.DebugContext(context.Background(), fmt.Sprint(args...))
}

func (a *slogAdapter) Info(args ...any) {
	a.logger.InfoContext(context.Background(), fmt.Sprint(args...))
}

func (a *slogAdapter) Warn(args ...any) {
	a.logger.WarnContext(context.Background(), fmt.Sprint(args...))
}

func (a *slogAdapter) Error(args ...any) {
	a.logger.ErrorContext(context.Background(), fmt.Sprint(args...))
}

func (a *slogAdapter) Fatal(args ...any) {
	a.logger.ErrorContext(context.Background(), fmt.Sprint(args...), slog.Bool("fatal", true))
	os.Exit(1)
}
