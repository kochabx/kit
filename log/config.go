package log

import (
	"github.com/kochabx/kit/log/writer"
)

// FileConfig 日志文件配置
type FileConfig struct {
	Filepath         string            `json:"filepath" default:"log"`
	Filename         string            `json:"filename" default:"app"`
	FileExt          string            `json:"file_ext" default:"log"`
	RotateMode       writer.RotateMode `json:"rotate_mode"`
	RotatelogsConfig RotatelogsConfig  `json:"rotatelogs_config"`
	LumberjackConfig LumberjackConfig  `json:"lumberjack_config"`
}

// RotatelogsConfig 按时间轮转配置
type RotatelogsConfig struct {
	MaxAge       int `json:"max_age" default:"24"`
	RotationTime int `json:"rotation_time" default:"1"`
}

// LumberjackConfig 按大小轮转配置
type LumberjackConfig struct {
	MaxSize    int  `json:"max_size" default:"100"`
	MaxBackups int  `json:"max_backups" default:"5"`
	MaxAge     int  `json:"max_age" default:"30"`
	Compress   bool `json:"compress" default:"false"`
}

// toWriterConfig 转换为 writer.RotateConfig
func (c *FileConfig) toWriterConfig() writer.RotateConfig {
	return writer.RotateConfig{
		Filepath: c.Filepath,
		Filename: c.Filename,
		FileExt:  c.FileExt,
		Mode:     c.RotateMode,
		TimeRotateConfig: writer.TimeRotateConfig{
			MaxAge:       c.RotatelogsConfig.MaxAge,
			RotationTime: c.RotatelogsConfig.RotationTime,
		},
		SizeRotateConfig: writer.SizeRotateConfig{
			MaxSize:    c.LumberjackConfig.MaxSize,
			MaxBackups: c.LumberjackConfig.MaxBackups,
			MaxAge:     c.LumberjackConfig.MaxAge,
			Compress:   c.LumberjackConfig.Compress,
		},
	}
}
