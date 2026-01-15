package log

import (
	"github.com/rs/zerolog"
)

var (
	// G 全局日志实例
	G *Logger
)

func init() {
	G = New()
}

// SetGlobalLogger 设置全局日志记录器
func SetGlobalLogger(logger *Logger) {
	G = logger
}

// SetGlobalLevel 设置全局日志级别
func SetGlobalLevel(level zerolog.Level) {
	G.Logger = G.Logger.Level(level)
}

// Debug 返回 debug 级别的日志事件
func Debug() *zerolog.Event {
	return G.Debug()
}

// Info 返回 info 级别的日志事件
func Info() *zerolog.Event {
	return G.Info()
}

// Warn 返回 warn 级别的日志事件
func Warn() *zerolog.Event {
	return G.Warn()
}

// Error 返回 error 级别的日志事件（带堆栈）
func Error() *zerolog.Event {
	return G.Error().Stack()
}

// Fatal 返回 fatal 级别的日志事件（带堆栈）
func Fatal() *zerolog.Event {
	return G.Fatal().Stack()
}

// Panic 返回 panic 级别的日志事件（带堆栈）
func Panic() *zerolog.Event {
	return G.Panic().Stack()
}

// Debugf 格式化输出 debug 日志
func Debugf(format string, args ...any) {
	G.Debug().Msgf(format, args...)
}

// Infof 格式化输出 info 日志
func Infof(format string, args ...any) {
	G.Info().Msgf(format, args...)
}

// Warnf 格式化输出 warn 日志
func Warnf(format string, args ...any) {
	G.Warn().Msgf(format, args...)
}

// Errorf 格式化输出 error 日志（带堆栈）
func Errorf(format string, args ...any) {
	G.Error().Stack().Msgf(format, args...)
}

// Fatalf 格式化输出 fatal 日志（带堆栈）
func Fatalf(format string, args ...any) {
	G.Fatal().Stack().Msgf(format, args...)
}

// Panicf 格式化输出 panic 日志（带堆栈）
func Panicf(format string, args ...any) {
	G.Panic().Stack().Msgf(format, args...)
}
