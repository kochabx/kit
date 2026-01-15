package writer

import (
	"fmt"
	"io"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"gopkg.in/natefinch/lumberjack.v2"
)

// RotateMode 日志轮转模式
type RotateMode int

const (
	// RotateModeTime 按时间轮转
	RotateModeTime RotateMode = iota
	// RotateModeSize 按大小轮转
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
func timeRotateWriter(config RotateConfig) (io.Writer, error) {
	writer, err := rotatelogs.New(
		config.fileFullPathWithFormat("%Y%m%d%H%M"),
		rotatelogs.WithLinkName(config.fileFullPath()),
		rotatelogs.WithMaxAge(time.Duration(config.TimeRotateConfig.MaxAge)*time.Hour),
		rotatelogs.WithRotationTime(time.Duration(config.TimeRotateConfig.RotationTime)*time.Hour),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create time rotate writer: %w", err)
	}
	return writer, nil
}

// sizeRotateWriter 按大小轮转的 writer
func sizeRotateWriter(config RotateConfig) (io.Writer, error) {
	return &lumberjack.Logger{
		Filename:   config.fileFullPath(),
		MaxSize:    config.SizeRotateConfig.MaxSize,
		MaxBackups: config.SizeRotateConfig.MaxBackups,
		MaxAge:     config.SizeRotateConfig.MaxAge,
		Compress:   config.SizeRotateConfig.Compress,
	}, nil
}
