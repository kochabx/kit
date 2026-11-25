// Package log provides structured slog logging with colored tint console output,
// dual size/time timberjack rotation, lock-free recursive redaction, and context extraction.
package log

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/DeRuina/timberjack"
	"github.com/lmittmann/tint"
)

// Logger wraps *slog.Logger alongside an optional Redactor and idempotent cleanup.
type Logger struct {
	slog      *slog.Logger
	levelVar  *slog.LevelVar
	redactor  *Redactor
	addSource bool
	closeFn   func() error
}

// New creates a colored tint console logger (defaulting to os.Stdout).
func New(opts ...Option) *Logger {
	cfg := buildOptions(opts...)
	handler := tintHandler(cfg.writer, &cfg, cfg.levelVar)
	return wrapHandler(handler, &cfg, nil)
}

// NewFile creates a JSON file logger backed by timberjack rotation.
func NewFile(fileCfg FileConfig, opts ...Option) (*Logger, error) {
	tj, err := openTimberjack(fileCfg)
	if err != nil {
		return nil, err
	}
	cfg := buildOptions(opts...)
	handler := jsonHandler(tj, &cfg, cfg.levelVar)
	return wrapHandler(handler, &cfg, []io.Closer{tj}), nil
}

// NewMulti creates a logger writing concurrently to console (tint) and a rotating JSON file.
func NewMulti(fileCfg FileConfig, opts ...Option) (*Logger, error) {
	tj, err := openTimberjack(fileCfg)
	if err != nil {
		return nil, err
	}
	cfg := buildOptions(opts...)
	consoleHandler := tintHandler(cfg.writer, &cfg, cfg.levelVar)
	fileHandler := jsonHandler(tj, &cfg, cfg.levelVar)
	return wrapHandler(slog.NewMultiHandler(consoleHandler, fileHandler), &cfg, []io.Closer{tj}), nil
}

func buildOptions(opts ...Option) struct {
	loggerOptions
	levelVar *slog.LevelVar
} {
	cfg := loggerOptions{
		level:      slog.LevelInfo,
		timeFormat: time.DateTime,
		writer:     os.Stdout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	lv := &slog.LevelVar{}
	lv.Set(cfg.level)
	return struct {
		loggerOptions
		levelVar *slog.LevelVar
	}{loggerOptions: cfg, levelVar: lv}
}

func tintHandler(w io.Writer, cfg *struct {
	loggerOptions
	levelVar *slog.LevelVar
}, level slog.Leveler) slog.Handler {
	return tint.NewTextHandler(w, &tint.Options{
		AddSource:  cfg.addSource,
		Level:      level,
		TimeFormat: cmp.Or(cfg.timeFormat, time.DateTime),
		NoColor:    cfg.noColor,
	})
}

func jsonHandler(w io.Writer, cfg *struct {
	loggerOptions
	levelVar *slog.LevelVar
}, level slog.Leveler) slog.Handler {
	layout := cmp.Or(cfg.timeFormat, time.DateTime)
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		AddSource: cfg.addSource,
		Level:     level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				a.Value = slog.StringValue(a.Value.Time().Format(layout))
			}
			return a
		},
	})
}

func openTimberjack(cfg FileConfig) (*timberjack.Logger, error) {
	if cfg.Filename == "" {
		return nil, errors.New("log: FileConfig.Filename is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Filename), 0o755); err != nil {
		return nil, fmt.Errorf("log: create log directory: %w", err)
	}
	tj := &timberjack.Logger{
		Filename:         cfg.Filename,
		MaxSize:          cfg.MaxSize,
		MaxBackups:       cfg.MaxBackups,
		MaxAge:           cfg.MaxAge,
		RotationInterval: cfg.RotationInterval,
		RotateAt:         slices.Clone(cfg.RotateAt),
		RotateAtMinutes:  slices.Clone(cfg.RotateAtMinutes),
		Compression:      cfg.Compression,
		LocalTime:        cfg.LocalTime,
		FileMode:         cfg.FileMode,
	}
	return tj, nil
}

func wrapHandler(base slog.Handler, cfg *struct {
	loggerOptions
	levelVar *slog.LevelVar
}, closers []io.Closer) *Logger {
	h := base
	if cfg.redactor != nil {
		h = cfg.redactor.Handler(h)
	}
	if len(cfg.extractors) > 0 {
		h = &contextHandler{extractors: slices.Clone(cfg.extractors), next: h}
	}
	closeFn := sync.OnceValue(func() error {
		var errs []error
		for _, c := range closers {
			if c != nil {
				errs = append(errs, c.Close())
			}
		}
		return errors.Join(errs...)
	})
	return &Logger{
		slog:      slog.New(h),
		levelVar:  cfg.levelVar,
		redactor:  cfg.redactor,
		addSource: cfg.addSource,
		closeFn:   closeFn,
	}
}

// Slog returns the underlying *slog.Logger for stdlib/x integration.
func (l *Logger) Slog() *slog.Logger { return l.slog }

// Redactor returns the configured Redactor, if any.
func (l *Logger) Redactor() *Redactor { return l.redactor }

// SetLevel updates the minimum log level at runtime across all outputs.
func (l *Logger) SetLevel(level slog.Level) { l.levelVar.Set(level) }

// Close idempotently flushes and closes owned file rotators.
func (l *Logger) Close() error {
	if l == nil || l.closeFn == nil {
		return nil
	}
	return l.closeFn()
}

// With returns a child Logger carrying persistent attributes.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{slog: l.slog.With(args...), levelVar: l.levelVar, redactor: l.redactor, addSource: l.addSource}
}

// WithGroup returns a child Logger opening an attribute group.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{slog: l.slog.WithGroup(name), levelVar: l.levelVar, redactor: l.redactor, addSource: l.addSource}
}

func (l *Logger) Debug(msg string, args ...any) {
	l.log(context.Background(), slog.LevelDebug, msg, args...)
}
func (l *Logger) Info(msg string, args ...any) {
	l.log(context.Background(), slog.LevelInfo, msg, args...)
}
func (l *Logger) Warn(msg string, args ...any) {
	l.log(context.Background(), slog.LevelWarn, msg, args...)
}
func (l *Logger) Error(msg string, args ...any) {
	l.log(context.Background(), slog.LevelError, msg, args...)
}
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelDebug, msg, args...)
}
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelWarn, msg, args...)
}
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelError, msg, args...)
}

func (l *Logger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if l == nil || l.slog == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.slog.Enabled(ctx, level) {
		return
	}
	var pc uintptr
	if l.addSource {
		var pcs [1]uintptr
		runtime.Callers(3, pcs[:])
		pc = pcs[0]
	}
	rec := slog.NewRecord(time.Now(), level, msg, pc)
	rec.Add(args...)
	_ = l.slog.Handler().Handle(ctx, rec)
}

type contextHandler struct {
	extractors []ContextExtractor
	next       slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{extractors: h.extractors, next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{extractors: h.extractors, next: h.next.WithGroup(name)}
}

func (h *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if ctx != nil {
		cloned := false
		for _, extract := range h.extractors {
			if attrs := extract(ctx); len(attrs) > 0 {
				if !cloned {
					rec = rec.Clone()
					cloned = true
				}
				rec.AddAttrs(attrs...)
			}
		}
	}
	return h.next.Handle(ctx, rec)
}
