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

// FileConfig configures file logging.
type FileConfig struct {
	Path       string           `json:"path" default:"log/app.log" validate:"required"`
	RotateMode RotateMode       `json:"rotate_mode" validate:"oneof=1 2"`
	TimeRotate TimeRotateConfig `json:"time_rotate"`
	SizeRotate SizeRotateConfig `json:"size_rotate"`
}

// TimeRotateConfig configures time-based rotation.
type TimeRotateConfig struct {
	MaxAge   time.Duration `json:"max_age" default:"24h" validate:"gt=0"`
	Interval time.Duration `json:"interval" default:"1h" validate:"gt=0"`
}

// SizeRotateConfig configures size-based rotation.
type SizeRotateConfig struct {
	MaxSize    int  `json:"max_size" default:"100" validate:"gt=0"`   // Maximum size of one log file in MB.
	MaxBackups int  `json:"max_backups" default:"5" validate:"gte=0"` // Number of old log files to retain.
	MaxAge     int  `json:"max_age" default:"30" validate:"gte=0"`    // Retention period in days.
	Compress   bool `json:"compress" default:"false"`                 // Whether to compress old log files.
}

// NewFile creates a file output writer.
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
