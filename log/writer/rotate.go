package writer

import (
	"fmt"
	"io"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"gopkg.in/natefinch/lumberjack.v2"
)

// RotateMode 日志轮转模式。
type RotateMode int

const (
	RotateModeUnknown RotateMode = iota
	// RotateModeTime 按时间轮转。
	RotateModeTime
	// RotateModeSize 按大小轮转。
	RotateModeSize
)

// String 返回轮转模式的字符串表示
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

// timeRotateWriter 按时间轮转的 writer
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

// sizeRotateWriter 按大小轮转的 writer
func sizeRotateWriter(config FileConfig) (io.Writer, error) {
	return &lumberjack.Logger{
		Filename:   config.Path,
		MaxSize:    config.SizeRotate.MaxSize,
		MaxBackups: config.SizeRotate.MaxBackups,
		MaxAge:     config.SizeRotate.MaxAge,
		Compress:   config.SizeRotate.Compress,
	}, nil
}
