package log

import (
	"github.com/rs/zerolog"

	"github.com/kochabx/kit/log/redact"
)

// Option configures a Logger.
type Option func(*loggerOptions)

type loggerOptions struct {
	level      *zerolog.Level
	caller     bool
	callerSkip int
	redactor   *redact.Redactor
}

// WithLevel sets the logging level.
func WithLevel(level zerolog.Level) Option {
	return func(o *loggerOptions) {
		o.level = new(level)
	}
}

// WithCaller enables caller information.
func WithCaller() Option {
	return func(o *loggerOptions) {
		o.caller = true
	}
}

// WithCallerSkip adds skip frames to the default caller depth.
// For example, skip=1 skips one additional wrapper around the logger.
func WithCallerSkip(skip int) Option {
	return func(o *loggerOptions) {
		o.caller = true
		o.callerSkip = skip
	}
}

// WithRedactor sets the log redactor.
func WithRedactor(redactor *redact.Redactor) Option {
	return func(o *loggerOptions) {
		o.redactor = redactor
	}
}
