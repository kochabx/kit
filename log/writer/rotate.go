package writer

import (
	"fmt"
	"io"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"gopkg.in/natefinch/lumberjack.v2"
)

// RotateMode selects the log rotation policy.
type RotateMode int

const (
	RotateModeUnknown RotateMode = iota
	// RotateModeTime selects time-based rotation.
	RotateModeTime
	// RotateModeSize selects size-based rotation.
	RotateModeSize
)

// String returns the rotation mode name.
func (m RotateMode) String() string {
	switch m {
	case RotateModeTime:
		return "time"
	case RotateModeSize:
		return "size"
	default:
		return "unknown"
	}
}

// timeRotateWriter creates a time-based rotating writer.
func timeRotateWriter(config FileConfig) (io.Writer, error) {
	writer, err := rotatelogs.New(
		config.rotatePattern(),
		rotatelogs.WithLinkName(config.Path),
		rotatelogs.WithMaxAge(config.TimeRotate.MaxAge),
		rotatelogs.WithRotationTime(config.TimeRotate.Interval),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create time rotate writer: %w", err)
	}
	return writer, nil
}

// sizeRotateWriter creates a size-based rotating writer.
func sizeRotateWriter(config FileConfig) (io.Writer, error) {
	return &lumberjack.Logger{
		Filename:   config.Path,
		MaxSize:    config.SizeRotate.MaxSize,
		MaxBackups: config.SizeRotate.MaxBackups,
		MaxAge:     config.SizeRotate.MaxAge,
		Compress:   config.SizeRotate.Compress,
	}, nil
}
