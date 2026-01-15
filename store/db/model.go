package db

import (
	"strconv"
	"strings"

	"github.com/kochabx/kit/core/stag"
)

// 数据库驱动类型
type Driver string

const (
	DriverMySQL      Driver = "mysql"
	DriverPostgreSQL Driver = "postgres"
	DriverSQLite     Driver = "sqlite"
)

// 日志级别
type Level int

const (
	LevelSilent Level = iota
	LevelError
	LevelWarn
	LevelInfo
)

// DriverConfig 接口定义
// 用于不同数据库驱动的配置实现
type DriverConfig interface {
	Driver() Driver
	Init() error
	Dsn() string
	LogLevel() Level
	CloneConn() *Connection
}

// Connection 连接池配置
type Connection struct {
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime int
}

// getLogLevel 获取日志级别
func getLogLevel(level string) Level {
	switch strings.ToLower(level) {
	case "silent":
		return LevelSilent
	case "error":
		return LevelError
	case "warn":
		return LevelWarn
	case "info":
		return LevelInfo
	default:
		return LevelSilent // 默认返回 silent
	}
}

// Mysql 配置
type MysqlConfig struct {
	Host            string `json:"host" default:"localhost"`
	Port            int    `json:"port" default:"3306"`
	User            string `json:"user" default:"root"`
	Password        string `json:"password"`
	Database        string `json:"database"`
	Charset         string `json:"charset" default:"utf8mb4"`
	ParseTime       bool   `json:"parseTime" default:"true"`
	Loc             string `json:"loc" default:"Local"`
	Timeout         int    `json:"timeout" default:"10"`
	MaxIdleConns    int    `json:"maxIdleConns" default:"10"`
	MaxOpenConns    int    `json:"maxOpenConns" default:"100"`
	ConnMaxLifetime int    `json:"connMaxLifetime" default:"3600"`
	Level           string `json:"level" default:"silent"`
}

func (c *MysqlConfig) Driver() Driver {
	return DriverMySQL
}

func (c *MysqlConfig) Init() error {
	return stag.ApplyDefaults(c)
}

func (c *MysqlConfig) Dsn() string {
	var builder strings.Builder
	builder.Grow(128)
	builder.WriteString(c.User)
	builder.WriteString(":")
	builder.WriteString(c.Password)
	builder.WriteString("@tcp(")
	builder.WriteString(c.Host)
	builder.WriteString(":")
	builder.WriteString(strconv.Itoa(c.Port))
	builder.WriteString(")/")
	builder.WriteString(c.Database)
	builder.WriteString("?charset=")
	builder.WriteString(c.Charset)
	builder.WriteString("&parseTime=")
	builder.WriteString(strconv.FormatBool(c.ParseTime))
	builder.WriteString("&loc=")
	builder.WriteString(c.Loc)
	builder.WriteString("&timeout=")
	builder.WriteString(strconv.Itoa(c.Timeout))
	builder.WriteString("s")
	return builder.String()
}

func (c *MysqlConfig) LogLevel() Level {
	return getLogLevel(c.Level)
}

func (c *MysqlConfig) CloneConn() *Connection {
	return &Connection{
		MaxIdleConns:    c.MaxIdleConns,
		MaxOpenConns:    c.MaxOpenConns,
		ConnMaxLifetime: c.ConnMaxLifetime,
	}
}

// PostgresConfig 配置
type PostgresConfig struct {
	Host            string `json:"host" default:"localhost"`
	Port            int    `json:"port" default:"5432"`
	User            string `json:"user" default:"postgres"`
	Password        string `json:"password"`
	Database        string `json:"database"`
	SSLMode         string `json:"sslmode" default:"disable"`
	ConnectTimeout  int    `json:"connectTimeout" default:"10"`
	MaxIdleConns    int    `json:"maxIdleConns" default:"10"`
	MaxOpenConns    int    `json:"maxOpenConns" default:"100"`
	ConnMaxLifetime int    `json:"connMaxLifetime" default:"3600"`
	Level           string `json:"level" default:"silent"`
}

func (c *PostgresConfig) Driver() Driver {
	return DriverPostgreSQL
}

func (c *PostgresConfig) Init() error {
	return stag.ApplyDefaults(c)
}

func (c *PostgresConfig) Dsn() string {
	var builder strings.Builder
	builder.Grow(128)
	builder.WriteString("host=")
	builder.WriteString(c.Host)
	builder.WriteString(" port=")
	builder.WriteString(strconv.Itoa(c.Port))
	builder.WriteString(" user=")
	builder.WriteString(c.User)
	builder.WriteString(" password=")
	builder.WriteString(c.Password)
	builder.WriteString(" dbname=")
	builder.WriteString(c.Database)
	builder.WriteString(" sslmode=")
	builder.WriteString(c.SSLMode)
	builder.WriteString(" connect_timeout=")
	builder.WriteString(strconv.Itoa(c.ConnectTimeout))
	return builder.String()
}

func (c *PostgresConfig) LogLevel() Level {
	return getLogLevel(c.Level)
}

func (c *PostgresConfig) CloneConn() *Connection {
	return &Connection{
		MaxIdleConns:    c.MaxIdleConns,
		MaxOpenConns:    c.MaxOpenConns,
		ConnMaxLifetime: c.ConnMaxLifetime,
	}
}

// SQLiteConfig 配置
type SQLiteConfig struct {
	FilePath        string `json:"filePath" default:"./data.db"`
	CacheSize       int    `json:"cacheSize" default:"10000"`
	BusyTimeout     int    `json:"busyTimeout" default:"5000"`
	SyncMode        string `json:"syncMode" default:"normal"`
	ForeignKeys     bool   `json:"foreignKeys" default:"true"`
	CacheMode       string `json:"cacheMode" default:"default"`
	MaxIdleConns    int    `json:"maxIdleConns" default:"10"`
	MaxOpenConns    int    `json:"maxOpenConns" default:"100"`
	ConnMaxLifetime int    `json:"connMaxLifetime" default:"3600"`
	Level           string `json:"level" default:"silent"`
}

func (c *SQLiteConfig) Driver() Driver {
	return DriverSQLite
}

func (c *SQLiteConfig) Init() error {
	return stag.ApplyDefaults(c)
}

func (c *SQLiteConfig) Dsn() string {
	var builder strings.Builder
	builder.Grow(128)
	builder.WriteString("file:")
	builder.WriteString(c.FilePath)
	builder.WriteString("?cache=")
	builder.WriteString(strconv.Itoa(c.CacheSize))
	builder.WriteString("&mode=rw&_busy_timeout=")
	builder.WriteString(strconv.Itoa(c.BusyTimeout))
	builder.WriteString("&_sync=")
	builder.WriteString(c.SyncMode)
	builder.WriteString("&_foreign_keys=")
	builder.WriteString(strconv.FormatBool(c.ForeignKeys))
	builder.WriteString("&_cache=")
	builder.WriteString(c.CacheMode)
	return builder.String()
}

func (c *SQLiteConfig) LogLevel() Level {
	return getLogLevel(c.Level)
}

func (c *SQLiteConfig) CloneConn() *Connection {
	return &Connection{
		MaxIdleConns:    c.MaxIdleConns,
		MaxOpenConns:    c.MaxOpenConns,
		ConnMaxLifetime: c.ConnMaxLifetime,
	}
}
