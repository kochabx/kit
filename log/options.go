package log

import (
	"github.com/rs/zerolog"

	"github.com/kochabx/kit/log/redact"
)

// Option Logger 选项函数
type Option func(*loggerOptions)

type loggerOptions struct {
	level      *zerolog.Level
	caller     bool
	callerSkip int
	redactor   *redact.Redactor
}

// WithLevel 设置日志级别
func WithLevel(level zerolog.Level) Option {
	return func(o *loggerOptions) {
		o.level = &level
	}
}

// WithCaller 设置调用栈信息
func WithCaller() Option {
	return func(o *loggerOptions) {
		o.caller = true
	}
}

// WithCallerSkip 在默认帧数基础上额外跳过的帧数
// skip=1 表示在 logger 之外额外封装了一层函数，以此类推
func WithCallerSkip(skip int) Option {
	return func(o *loggerOptions) {
		o.caller = true
		o.callerSkip = skip
	}
}

// WithRedactor 设置日志脱敏器。
func WithRedactor(redactor *redact.Redactor) Option {
	return func(o *loggerOptions) {
		o.redactor = redactor
	}
}
