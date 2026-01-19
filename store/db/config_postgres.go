package db

import (
	"strconv"
	"strings"
	"time"

	"github.com/kochabx/kit/core/tag"
)

// PostgresConfig PostgreSQL 数据库配置
type PostgresConfig struct {
	// 连接配置
	Host     string `json:"host" default:"localhost"`
	Port     int    `json:"port" default:"5432"`
	User     string `json:"user" default:"postgres"`
	Password string `json:"password"`
	Database string `json:"database"`

	// PostgreSQL 特有配置
	SSLMode        string `json:"sslmode" default:"disable"`
	TimeZone       string `json:"timezone" default:"Asia/Shanghai"`
	ConnectTimeout int    `json:"connectTimeout" default:"10"`

	// 连接池配置
	PoolConfig `json:"pool"`

	// 日志级别
	Level string `json:"level" default:"silent"`

	// 内部标记
	initialized bool
}

// Driver 返回 PostgreSQL 驱动类型
func (c *PostgresConfig) Driver() Driver {
	return DriverPostgres
}

// Init 初始化配置，应用默认值
func (c *PostgresConfig) Init() error {
	if c.initialized {
		return nil
	}
	if err := tag.ApplyDefaults(c); err != nil {
		return err
	}
	c.initialized = true
	return nil
}

// DSN 生成 PostgreSQL DSN 连接字符串
func (c *PostgresConfig) DSN() string {
	var b strings.Builder
	b.Grow(128)

	b.WriteString("host=")
	b.WriteString(c.Host)
	b.WriteString(" port=")
	b.WriteString(strconv.Itoa(c.Port))
	b.WriteString(" user=")
	b.WriteString(c.User)
	b.WriteString(" password=")
	b.WriteString(c.Password)
	b.WriteString(" dbname=")
	b.WriteString(c.Database)
	b.WriteString(" sslmode=")
	b.WriteString(c.SSLMode)
	b.WriteString(" TimeZone=")
	b.WriteString(c.TimeZone)
	b.WriteString(" connect_timeout=")
	b.WriteString(strconv.Itoa(c.ConnectTimeout))

	return b.String()
}

// Pool 返回连接池配置
func (c *PostgresConfig) Pool() *PoolConfig {
	pool := &c.PoolConfig
	if pool.MaxIdleConns == 0 {
		pool.MaxIdleConns = 10
	}
	if pool.MaxOpenConns == 0 {
		pool.MaxOpenConns = 100
	}
	if pool.ConnMaxLifetime == 0 {
		pool.ConnMaxLifetime = time.Hour
	}
	if pool.ConnMaxIdleTime == 0 {
		pool.ConnMaxIdleTime = 10 * time.Minute
	}
	return pool
}

// LogLevel 返回日志级别
func (c *PostgresConfig) LogLevel() LogLevel {
	return ParseLogLevel(c.Level)
}
