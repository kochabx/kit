package log

import (
	"context"
	"log/slog"
	"sync/atomic"
)

var defaultLogger atomic.Pointer[Logger]

func init() {
	defaultLogger.Store(New())
}

// Default returns the package-level global Logger.
func Default() *Logger {
	return defaultLogger.Load()
}

// SetDefault atomically replaces the package-level global Logger (no-op if l is nil).
func SetDefault(l *Logger) {
	if l != nil {
		defaultLogger.Store(l)
	}
}

// SetLevel updates the minimum log level on the global Logger.
func SetLevel(level slog.Level) {
	if l := Default(); l != nil {
		l.SetLevel(level)
	}
}

// With returns a child Logger from the global Logger carrying persistent attributes.
func With(args ...any) *Logger {
	return Default().With(args...)
}

// WithGroup returns a child Logger from the global Logger opening an attribute group.
func WithGroup(name string) *Logger {
	return Default().WithGroup(name)
}

func Debug(msg string, args ...any) {
	Default().log(context.Background(), slog.LevelDebug, msg, args...)
}

func Info(msg string, args ...any) {
	Default().log(context.Background(), slog.LevelInfo, msg, args...)
}

func Warn(msg string, args ...any) {
	Default().log(context.Background(), slog.LevelWarn, msg, args...)
}

func Error(msg string, args ...any) {
	Default().log(context.Background(), slog.LevelError, msg, args...)
}

func DebugContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, slog.LevelDebug, msg, args...)
}

func InfoContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, slog.LevelInfo, msg, args...)
}

func WarnContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, slog.LevelWarn, msg, args...)
}

func ErrorContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, slog.LevelError, msg, args...)
}
