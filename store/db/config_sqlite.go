package db

import (
	"strconv"
	"strings"
	"time"

	"github.com/kochabx/kit/core/tag"
)

// SQLiteConfig SQLite 数据库配置
type SQLiteConfig struct {
	// 文件路径
	FilePath string `json:"filePath" default:"./data.db"`

	// SQLite 特有配置
	JournalMode string        `json:"journalMode" default:"WAL"`
	CacheSize   int           `json:"cacheSize" default:"-2000"`
	BusyTimeout time.Duration `json:"busyTimeout" default:"5s"`
	SyncMode    string        `json:"syncMode" default:"NORMAL"`
	ForeignKeys bool          `json:"foreignKeys" default:"true"`

	// 连接池配置
	PoolConfig `json:"pool"`

	// 日志级别
	Level string `json:"level" default:"silent"`

	// 内部标记
	initialized bool
}

// Driver 返回 SQLite 驱动类型
func (c *SQLiteConfig) Driver() Driver {
	return DriverSQLite
}

// Init 初始化配置，应用默认值
func (c *SQLiteConfig) Init() error {
	if c.initialized {
		return nil
	}
	if err := tag.ApplyDefaults(c); err != nil {
		return err
	}
	c.initialized = true
	return nil
}

// DSN 生成 SQLite DSN 连接字符串
func (c *SQLiteConfig) DSN() string {
	var b strings.Builder
	b.Grow(128)

	b.WriteString("file:")
	b.WriteString(c.FilePath)
	b.WriteString("?_journal_mode=")
	b.WriteString(c.JournalMode)
	b.WriteString("&_cache_size=")
	b.WriteString(strconv.Itoa(c.CacheSize))
	b.WriteString("&_busy_timeout=")
	b.WriteString(strconv.FormatInt(c.BusyTimeout.Milliseconds(), 10))
	b.WriteString("&_synchronous=")
	b.WriteString(c.SyncMode)
	b.WriteString("&_foreign_keys=")
	b.WriteString(strconv.FormatBool(c.ForeignKeys))

	return b.String()
}

// Pool 返回连接池配置
func (c *SQLiteConfig) Pool() *PoolConfig {
	// SQLite 单文件数据库，使用单连接
	c.PoolConfig.MaxIdleConns = 1
	c.PoolConfig.MaxOpenConns = 1
	return &c.PoolConfig
}

// LogLevel 返回日志级别
func (c *SQLiteConfig) LogLevel() LogLevel {
	return ParseLogLevel(c.Level)
}
