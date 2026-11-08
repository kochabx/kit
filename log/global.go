package log

import (
	"sync/atomic"

	"github.com/rs/zerolog"
)

var global atomic.Pointer[Logger]

func init() {
	global.Store(New())
}

// Global returns the current global logger.
func Global() *Logger {
	return global.Load()
}

// SetGlobal atomically replaces the global logger and returns the previous instance.
// The caller decides whether and when to close the previous instance.
func SetGlobal(logger *Logger) *Logger {
	if logger == nil {
		panic("log: nil global logger")
	}
	return global.Swap(logger)
}

// Debug creates a debug-level event.
func Debug() *zerolog.Event {
	return Global().Debug()
}

// Info creates an info-level event.
func Info() *zerolog.Event {
	return Global().Info()
}

// Warn creates a warning-level event.
func Warn() *zerolog.Event {
	return Global().Warn()
}

// Error creates an error-level event.
func Error() *zerolog.Event {
	return Global().Error()
}

// Fatal creates a fatal-level event.
func Fatal() *zerolog.Event {
	return Global().Fatal()
}

// Panic creates a panic-level event.
func Panic() *zerolog.Event {
	return Global().Panic()
}
