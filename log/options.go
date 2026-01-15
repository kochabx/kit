package log

import (
	"github.com/rs/zerolog"

	"github.com/kochabx/kit/log/desensitize"
)

// Option Logger 选项函数
type Option func(*Logger)

// WithLevel 设置日志级别
func WithLevel(level zerolog.Level) Option {
	return func(l *Logger) {
		l.Logger = l.Logger.Level(level)
	}
}

// WithCaller 设置调用栈信息
func WithCaller() Option {
	return func(l *Logger) {
		l.Logger = l.Logger.With().Caller().Logger()
	}
}

// WithCallerSkip 设置调用栈跳过的帧数
func WithCallerSkip(skip int) Option {
	return func(l *Logger) {
		l.Logger = l.Logger.With().CallerWithSkipFrameCount(skip).Logger()
	}
}

// WithDesensitize 设置脱敏钩子
func WithDesensitize(hook *desensitize.Hook) Option {
	return func(l *Logger) {
		l.desensitizeHook = hook
	}
}
