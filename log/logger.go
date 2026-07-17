package log

import (
	"io"

	"github.com/rs/zerolog"

	"github.com/kochabx/kit/log/redact"
	"github.com/kochabx/kit/log/writer"
)

// Logger wraps zerolog with optional redaction and resource cleanup.
type Logger struct {
	zerolog.Logger
	redactor *redact.Redactor
	closer   io.Closer
}

// Redactor returns the configured log redactor.
func (l *Logger) Redactor() *redact.Redactor {
	return l.redactor
}

// Close releases resources owned by the logger.
func (l *Logger) Close() error {
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

func newWithWriter(w io.Writer, opts ...Option) *Logger {
	config := loggerOptions{}
	for _, opt := range opts {
		opt(&config)
	}

	output := w
	if config.redactor != nil {
		output = redact.NewWriter(w, config.redactor)
	}

	zlogger := zerolog.New(output).With().Timestamp().Logger()
	if config.level != nil {
		zlogger = zlogger.Level(*config.level)
	}
	if config.caller {
		skip := zerolog.CallerSkipFrameCount + config.callerSkip
		zlogger = zlogger.With().CallerWithSkipFrameCount(skip).Logger()
	}

	return &Logger{Logger: zlogger, redactor: config.redactor}
}

// New creates a console logger.
func New(opts ...Option) *Logger {
	return newWithWriter(writer.NewConsole(), opts...)
}
