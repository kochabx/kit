package writer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
)

// FileConfig 文件日志配置。
type FileConfig struct {
	Path       string           `json:"path" default:"log/app.log" validate:"required"`
	RotateMode RotateMode       `json:"rotate_mode" validate:"oneof=1 2"`
	TimeRotate TimeRotateConfig `json:"time_rotate"`
	SizeRotate SizeRotateConfig `json:"size_rotate"`
}

// TimeRotateConfig 按时间轮转配置。
type TimeRotateConfig struct {
	MaxAge   time.Duration `json:"max_age" default:"24h" validate:"gt=0"`
	Interval time.Duration `json:"interval" default:"1h" validate:"gt=0"`
}

// SizeRotateConfig 按大小轮转配置。
type SizeRotateConfig struct {
	MaxSize    int  `json:"max_size" default:"100" validate:"gt=0"`   // 单个日志文件最大大小（MB）
	MaxBackups int  `json:"max_backups" default:"5" validate:"gte=0"` // 保留的旧日志文件数量
	MaxAge     int  `json:"max_age" default:"30" validate:"gte=0"`    // 日志文件保留天数
	Compress   bool `json:"compress" default:"false"`                 // 是否压缩旧日志文件
}

// File 创建文件输出 writer
func NewFile(config FileConfig) (io.Writer, error) {
	if err := defaults.Apply(&config); err != nil {
		return nil, err
	}
	if err := validator.Validate.Struct(context.Background(), &config); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(config.Path), 0o755); err != nil {
		return nil, err
	}

	switch config.RotateMode {
	case RotateModeTime:
		return timeRotateWriter(config)
	case RotateModeSize:
		return sizeRotateWriter(config)
	default:
		return nil, fmt.Errorf("unsupported rotate mode: %v", config.RotateMode)
	}
}

func (c FileConfig) rotatePattern() string {
	ext := filepath.Ext(c.Path)
	base := c.Path[:len(c.Path)-len(ext)]
	return base + ".%Y%m%d%H%M" + ext
}
