package gorm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	gormlib "gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type slogLogger struct {
	logger            *slog.Logger
	level             gormlogger.LogLevel
	slowThreshold     time.Duration
	logRecordNotFound bool
}

func newSlogLogger(logger *slog.Logger, slowThreshold time.Duration, logRecordNotFound bool) *slogLogger {
	return &slogLogger{
		logger:            logger,
		level:             gormlogger.Warn,
		slowThreshold:     slowThreshold,
		logRecordNotFound: logRecordNotFound,
	}
}

func (l *slogLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	cp := *l
	cp.level = level
	return &cp
}

// ParamsFilter keeps SQL placeholders unexpanded so bound parameters never enter query logs.
func (l *slogLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

func (l *slogLogger) Info(ctx context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Info {
		l.logger.InfoContext(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l *slogLogger) Warn(ctx context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Warn {
		l.logger.WarnContext(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l *slogLogger) Error(ctx context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Error {
		l.logger.ErrorContext(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l *slogLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	isErr := err != nil && l.level >= gormlogger.Error && (!errors.Is(err, gormlib.ErrRecordNotFound) || l.logRecordNotFound)
	isSlow := l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= gormlogger.Warn
	isDebug := l.level >= gormlogger.Info
	if !isErr && !isSlow && !isDebug {
		return
	}

	sql, rows := fc()
	switch {
	case isErr:
		l.logger.ErrorContext(ctx, "gorm query error",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
			slog.Any("err", err),
		)
	case isSlow:
		l.logger.WarnContext(ctx, "slow gorm query",
			slog.Duration("elapsed", elapsed),
			slog.Duration("threshold", l.slowThreshold),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	case isDebug:
		l.logger.DebugContext(ctx, "gorm query",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	}
}
