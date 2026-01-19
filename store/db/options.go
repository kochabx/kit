package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/kochabx/kit/log"
)

// Option 客户端配置选项
type Option func(*clientOptions)

// clientOptions 客户端内部选项
type clientOptions struct {
	// 日志
	logger *log.Logger

	// 插件
	plugins []gorm.Plugin

	// 连接超时
	connectTimeout time.Duration

	// 慢查询阈值（0 表示禁用）
	slowQueryThresh time.Duration

	// 自定义 GORM 配置
	gormConfig *gorm.Config
}

// defaultOptions 返回默认选项
func defaultOptions() *clientOptions {
	return &clientOptions{
		connectTimeout: 10 * time.Second,
		// slowQueryThresh 默认为 0（禁用）
	}
}

// ==================== 日志选项 ====================

// WithLogger 设置日志记录器
func WithLogger(l *log.Logger) Option {
	return func(o *clientOptions) {
		o.logger = l
	}
}

// ==================== 插件选项 ====================

// WithPlugins 添加 GORM 插件
func WithPlugins(plugins ...gorm.Plugin) Option {
	return func(o *clientOptions) {
		o.plugins = append(o.plugins, plugins...)
	}
}

// ==================== 连接选项 ====================

// WithConnectTimeout 设置连接超时时间
func WithConnectTimeout(d time.Duration) Option {
	return func(o *clientOptions) {
		if d > 0 {
			o.connectTimeout = d
		}
	}
}

// ==================== 可观测性选项 ====================

// WithSlowQuery 启用慢查询日志（threshold 为 0 表示禁用）
func WithSlowQuery(threshold time.Duration) Option {
	return func(o *clientOptions) {
		o.slowQueryThresh = threshold
	}
}

// ==================== 高级选项 ====================

// WithGormConfig 设置自定义 GORM 配置
func WithGormConfig(cfg *gorm.Config) Option {
	return func(o *clientOptions) {
		o.gormConfig = cfg
	}
}
