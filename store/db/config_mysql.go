package db

import (
	"strconv"
	"strings"
	"time"

	"github.com/kochabx/kit/core/tag"
)

// MySQLConfig MySQL 数据库配置
type MySQLConfig struct {
	// 连接配置
	Host     string `json:"host" default:"localhost"`
	Port     int    `json:"port" default:"3306"`
	User     string `json:"user" default:"root"`
	Password string `json:"password"`
	Database string `json:"database"`

	// MySQL 特有配置
	Charset   string        `json:"charset" default:"utf8mb4"`
	Collation string        `json:"collation" default:"utf8mb4_unicode_ci"`
	ParseTime bool          `json:"parseTime" default:"true"`
	Loc       string        `json:"loc" default:"Local"`
	Timeout   time.Duration `json:"timeout" default:"10s"`

	// 连接池配置
	PoolConfig `json:"pool"`

	// 日志级别
	Level string `json:"level" default:"silent"`

	// 内部标记
	initialized bool
}

// Driver 返回 MySQL 驱动类型
func (c *MySQLConfig) Driver() Driver {
	return DriverMySQL
}

// Init 初始化配置，应用默认值
func (c *MySQLConfig) Init() error {
	if c.initialized {
		return nil
	}
	if err := tag.ApplyDefaults(c); err != nil {
		return err
	}
	c.initialized = true
	return nil
}

// DSN 生成 MySQL DSN 连接字符串
func (c *MySQLConfig) DSN() string {
	var b strings.Builder
	b.Grow(128)

	// user:password@tcp(host:port)/database
	b.WriteString(c.User)
	b.WriteByte(':')
	b.WriteString(c.Password)
	b.WriteString("@tcp(")
	b.WriteString(c.Host)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(c.Port))
	b.WriteString(")/")
	b.WriteString(c.Database)

	// 参数
	b.WriteString("?charset=")
	b.WriteString(c.Charset)
	b.WriteString("&collation=")
	b.WriteString(c.Collation)
	b.WriteString("&parseTime=")
	b.WriteString(strconv.FormatBool(c.ParseTime))
	b.WriteString("&loc=")
	b.WriteString(c.Loc)
	b.WriteString("&timeout=")
	b.WriteString(c.Timeout.String())

	return b.String()
}

// Pool 返回连接池配置
func (c *MySQLConfig) Pool() *PoolConfig {
	return &c.PoolConfig
}

// LogLevel 返回日志级别
func (c *MySQLConfig) LogLevel() LogLevel {
	return ParseLogLevel(c.Level)
}
