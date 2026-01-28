package db

import "errors"

var (
	// ErrUnsupportedDriver 不支持的数据库驱动
	ErrUnsupportedDriver = errors.New("db: unsupported driver")

	// ErrInvalidConfig 无效的数据库配置
	ErrInvalidConfig = errors.New("db: invalid config")

	// ErrNotInitialized 数据库未初始化
	ErrNotInitialized = errors.New("db: not initialized")
)
