package db

import (
	"strings"
	"time"
)

// Driver 数据库驱动类型
type Driver string

const (
	// DriverMySQL MySQL 驱动
	DriverMySQL Driver = "mysql"
	// DriverPostgres PostgreSQL 驱动
	DriverPostgres Driver = "postgres"
	// DriverSQLite SQLite 驱动
	DriverSQLite Driver = "sqlite"
)

// String 返回驱动名称
func (d Driver) String() string {
	return string(d)
}

// LogLevel 日志级别
type LogLevel int

const (
	// LogLevelSilent 静默模式
	LogLevelSilent LogLevel = iota
	// LogLevelError 错误级别
	LogLevelError
	// LogLevelWarn 警告级别
	LogLevelWarn
	// LogLevelInfo 信息级别
	LogLevelInfo
)

// PoolConfig 连接池配置
type PoolConfig struct {
	// MaxIdleConns 最大空闲连接数
	MaxIdleConns int `json:"maxIdleConns" default:"10"`

	// MaxOpenConns 最大打开连接数
	MaxOpenConns int `json:"maxOpenConns" default:"100"`

	// ConnMaxLifetime 连接最大生命周期
	ConnMaxLifetime time.Duration `json:"connMaxLifetime" default:"1h"`

	// ConnMaxIdleTime 连接最大空闲时间
	ConnMaxIdleTime time.Duration `json:"connMaxIdleTime" default:"10m"`
}

// DriverConfig 驱动配置接口
type DriverConfig interface {
	// Driver 返回驱动类型
	Driver() Driver

	// DSN 返回数据源名称
	DSN() string

	// Pool 返回连接池配置
	Pool() *PoolConfig

	// Init 初始化配置（应用默认值）
	Init() error

	// LogLevel 返回日志级别
	LogLevel() LogLevel
}

// ParseLogLevel 解析日志级别字符串
func ParseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "silent":
		return LogLevelSilent
	case "error":
		return LogLevelError
	case "warn":
		return LogLevelWarn
	case "info":
		return LogLevelInfo
	default:
		return LogLevelSilent
	}
}
