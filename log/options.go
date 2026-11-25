package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"
)

// ContextExtractor extracts structured attributes (e.g. trace/span IDs) from ctx.
type ContextExtractor func(context.Context) []slog.Attr

// FileConfig configures size- and time-based log file rotation via timberjack.
type FileConfig struct {
	Filename         string
	MaxSize          int           // Megabytes per file before rotation (timberjack default: 100).
	MaxBackups       int           // Retained old log files (0 preserves all unless MaxAge prunes).
	MaxAge           int           // Maximum days to retain old files.
	RotationInterval time.Duration // Time-based rotation interval (0 disables interval rotation).
	RotateAt         []string      // Daily HH:MM rotation triggers (e.g. "00:00").
	RotateAtMinutes  []int         // Minute marks (0-59) within an hour to trigger rotation.
	Compression      string        // "none", "gzip", or "zstd".
	LocalTime        bool
	FileMode         os.FileMode
}

type loggerOptions struct {
	level      slog.Level
	addSource  bool
	noColor    bool
	timeFormat string
	writer     io.Writer
	redactor   *Redactor
	extractors []ContextExtractor
}

// Option configures Logger construction.
type Option func(*loggerOptions)

// WithLevel sets the minimum log level (default: slog.LevelInfo).
func WithLevel(level slog.Level) Option {
	return func(o *loggerOptions) { o.level = level }
}

// WithAddSource enables source file/line attribution.
func WithAddSource(enabled bool) Option {
	return func(o *loggerOptions) { o.addSource = enabled }
}

// WithNoColor disables ANSI color escapes in console output.
func WithNoColor(disabled bool) Option {
	return func(o *loggerOptions) { o.noColor = disabled }
}

// WithTimeFormat sets the timestamp layout for console and JSON logs (default: time.DateTime).
func WithTimeFormat(layout string) Option {
	return func(o *loggerOptions) {
		if layout != "" {
			o.timeFormat = layout
		}
	}
}

// WithWriter overrides the console output io.Writer (default: os.Stdout).
func WithWriter(w io.Writer) Option {
	return func(o *loggerOptions) {
		if w != nil {
			o.writer = w
		}
	}
}

// WithRedactor attaches a lock-free dynamic Redactor to the logging pipeline.
func WithRedactor(r *Redactor) Option {
	return func(o *loggerOptions) { o.redactor = r }
}

// WithContextExtractor registers a context attribute extractor (e.g. OTel trace bridge).
func WithContextExtractor(fn ContextExtractor) Option {
	return func(o *loggerOptions) {
		if fn != nil {
			o.extractors = append(o.extractors, fn)
		}
	}
}
