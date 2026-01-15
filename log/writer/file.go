package writer

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// RotateConfig 日志轮转配置
type RotateConfig struct {
	Mode             RotateMode
	Filepath         string
	Filename         string
	FileExt          string
	TimeRotateConfig TimeRotateConfig
	SizeRotateConfig SizeRotateConfig
}

// TimeRotateConfig 按时间轮转配置
type TimeRotateConfig struct {
	MaxAge       int // 日志保留时间(小时)
	RotationTime int // 轮转时间间隔(小时)
}

// SizeRotateConfig 按大小轮转配置
type SizeRotateConfig struct {
	MaxSize    int  // 单个日志文件最大大小(MB)
	MaxBackups int  // 保留的旧日志文件数量
	MaxAge     int  // 日志文件保留天数
	Compress   bool // 是否压缩旧日志文件
}

// File 创建文件输出 writer
func File(config RotateConfig) (io.Writer, error) {
	switch config.Mode {
	case RotateModeTime:
		return timeRotateWriter(config)
	case RotateModeSize:
		return sizeRotateWriter(config)
	default:
		return nil, fmt.Errorf("unsupported rotate mode: %v", config.Mode)
	}
}

// fileFullPath 返回日志文件的完整路径
func (c *RotateConfig) fileFullPath() string {
	return c.fileFullPathWithFormat("")
}

// fileFullPathWithFormat 返回带格式的日志文件完整路径
func (c *RotateConfig) fileFullPathWithFormat(format string) string {
	var builder strings.Builder
	builder.Grow(len(c.Filename) + len(format) + len(c.FileExt) + 3)

	builder.WriteString(c.Filename)
	if format != "" {
		builder.WriteByte('.')
		builder.WriteString(format)
	}
	builder.WriteByte('.')
	builder.WriteString(c.FileExt)

	return filepath.Join(c.Filepath, builder.String())
}
