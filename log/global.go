package log

import (
	"sync/atomic"

	"github.com/rs/zerolog"
)

var global atomic.Pointer[Logger]

func init() {
	global.Store(New())
}

// Global 返回当前全局日志记录器。
func Global() *Logger {
	return global.Load()
}

// SetGlobal 原子替换全局日志记录器并返回旧实例。
// 调用方负责决定是否关闭返回的旧实例。
func SetGlobal(logger *Logger) *Logger {
	if logger == nil {
		panic("log: nil global logger")
	}
	return global.Swap(logger)
}

// Debug 返回 debug 级别的日志事件
func Debug() *zerolog.Event {
	return Global().Debug()
}

// Info 返回 info 级别的日志事件
func Info() *zerolog.Event {
	return Global().Info()
}

// Warn 返回 warn 级别的日志事件
func Warn() *zerolog.Event {
	return Global().Warn()
}

// Error 返回 error 级别的日志事件
func Error() *zerolog.Event {
	return Global().Error()
}

// Fatal 返回 fatal 级别的日志事件
func Fatal() *zerolog.Event {
	return Global().Fatal()
}

// Panic 返回 panic 级别的日志事件
func Panic() *zerolog.Event {
	return Global().Panic()
}
