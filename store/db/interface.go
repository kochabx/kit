package db

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

// Database 数据库客户端接口
type Database interface {
	// DB 获取底层 GORM 数据库实例
	DB() *gorm.DB

	// Ping 测试数据库连接
	Ping(ctx context.Context) error

	// Close 关闭数据库连接
	Close() error

	// Stats 获取连接池统计信息
	Stats() sql.DBStats

	// IsHealthy 返回健康状态
	IsHealthy() bool
}

// 确保 Client 实现 Database 接口
var _ Database = (*Client)(nil)
