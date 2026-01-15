package log

import (
	"fmt"
	"io"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"

	"github.com/kochabx/kit/core/tag"
	"github.com/kochabx/kit/log/desensitize"
	"github.com/kochabx/kit/log/writer"
)

// Logger 日志记录器
type Logger struct {
	zerolog.Logger
	desensitizeHook *desensitize.Hook
	writer          io.Writer
	closer          io.Closer // 用于资源清理
}

// GetDesensitizeHook 获取脱敏钩子
func (l *Logger) GetDesensitizeHook() *desensitize.Hook {
	return l.desensitizeHook
}

// Close 关闭日志记录器，释放资源
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

func init() {
	// 初始化全局日志配置
	zerolog.TimeFieldFormat = time.DateTime
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
}

// SetZerologGlobalLevel 设置全局日志级别
func SetZerologGlobalLevel(level zerolog.Level) {
	zerolog.SetGlobalLevel(level)
}

// newLogger 统一的 Logger 构建方法
func newLogger(w io.Writer, opts ...Option) *Logger {
	logger := &Logger{
		writer: w,
		Logger: zerolog.New(w).With().Timestamp().Logger(),
	}

	// 应用所有选项
	for _, opt := range opts {
		opt(logger)
	}

	// 如果设置了脱敏钩子，包装 writer
	if logger.desensitizeHook != nil {
		dw := desensitize.NewWriter(w, logger.desensitizeHook)
		logger.Logger = zerolog.New(dw).With().Timestamp().Logger()

		// 重新应用选项（因为重建了 Logger）
		for _, opt := range opts {
			opt(logger)
		}
	}

	return logger
}

// New 创建新的 Logger 实例，输出到控制台
func New(opts ...Option) *Logger {
	return newLogger(writer.Console(), opts...)
}

// NewFile 创建文件输出的 Logger
func NewFile(c FileConfig, opts ...Option) (*Logger, error) {
	// 应用默认配置
	if err := tag.ApplyDefaults(&c); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// 创建文件 writer
	w, err := writer.File(c.toWriterConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create file writer: %w", err)
	}

	logger := newLogger(w, opts...)

	// 如果 writer 实现了 io.Closer，保存以便后续清理
	if closer, ok := w.(io.Closer); ok {
		logger.closer = closer
	}

	return logger, nil
}

// NewMulti 创建同时输出到文件和控制台的 Logger
func NewMulti(c FileConfig, opts ...Option) (*Logger, error) {
	// 应用默认配置
	if err := tag.ApplyDefaults(&c); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	// 创建文件 writer
	fw, err := writer.File(c.toWriterConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create file writer: %w", err)
	}

	// 创建多路输出
	multi := zerolog.MultiLevelWriter(fw, writer.Console())
	logger := newLogger(multi, opts...)

	// 如果文件 writer 实现了 io.Closer，保存以便后续清理
	if closer, ok := fw.(io.Closer); ok {
		logger.closer = closer
	}

	return logger, nil
}
