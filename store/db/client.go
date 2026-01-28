package db

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kochabx/kit/log"
)

// Client 数据库客户端
type Client struct {
	config  DriverConfig
	db      *gorm.DB
	sqlDB   *sql.DB
	options *clientOptions
	logger  *log.Logger
}

// New 创建新的数据库客户端
func New(cfg DriverConfig, opts ...Option) (*Client, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}

	// 初始化配置
	if err := cfg.Init(); err != nil {
		return nil, err
	}

	// 应用选项
	options := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}

	c := &Client{
		config:  cfg,
		options: options,
		logger:  options.logger,
	}

	// 使用默认全局日志
	if c.logger == nil {
		c.logger = log.G
	}

	// 创建数据库连接
	if err := c.connect(); err != nil {
		return nil, err
	}

	// 测试连接
	pingCtx, pingCancel := context.WithTimeout(context.Background(), options.connectTimeout)
	defer pingCancel()

	if err := c.Ping(pingCtx); err != nil {
		_ = c.Close()
		return nil, err
	}

	c.logger.Debug().
		Str("driver", cfg.Driver().String()).
		Msg("database client created")

	return c, nil
}

// connect 创建数据库连接
func (c *Client) connect() error {
	// 构建 GORM 配置
	gormConfig := c.buildGormConfig()

	// 获取 Dialector
	dialector, err := c.getDialector()
	if err != nil {
		return err
	}

	// 打开连接
	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return err
	}

	// 获取底层 sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	// 配置连接池
	c.configurePool(sqlDB)

	// 应用插件
	if err := c.usePlugins(db); err != nil {
		return err
	}

	c.db = db
	c.sqlDB = sqlDB
	return nil
}

// getDialector 获取 GORM Dialector
func (c *Client) getDialector() (gorm.Dialector, error) {
	dsn := c.config.DSN()

	switch c.config.Driver() {
	case DriverMySQL:
		return mysql.Open(dsn), nil
	case DriverPostgres:
		return postgres.Open(dsn), nil
	case DriverSQLite:
		return sqlite.Open(dsn), nil
	default:
		return nil, ErrUnsupportedDriver
	}
}

// buildGormConfig 构建 GORM 配置
func (c *Client) buildGormConfig() *gorm.Config {
	if c.options.gormConfig != nil {
		return c.options.gormConfig
	}

	cfg := &gorm.Config{}

	// 使用 GORM 原生 logger
	loggerConfig := logger.Config{
		LogLevel:                  logger.LogLevel(c.config.LogLevel()),
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	}
	// 慢查询阈值大于 0 时启用
	if c.options.slowQueryThresh > 0 {
		loggerConfig.SlowThreshold = c.options.slowQueryThresh
	}
	cfg.Logger = logger.New(newGormLogWriter(c.logger), loggerConfig)

	return cfg
}

// configurePool 配置连接池
func (c *Client) configurePool(sqlDB *sql.DB) {
	pool := c.config.Pool()
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
}

// usePlugins 应用插件
func (c *Client) usePlugins(db *gorm.DB) error {
	for _, plugin := range c.options.plugins {
		if err := db.Use(plugin); err != nil {
			return err
		}
	}
	return nil
}

// DB 获取 GORM 数据库实例
func (c *Client) DB() *gorm.DB {
	return c.db
}

// Ping 测试数据库连接
func (c *Client) Ping(ctx context.Context) error {
	if c.sqlDB == nil {
		return ErrNotInitialized
	}

	return c.sqlDB.PingContext(ctx)
}

// Close 关闭数据库连接
func (c *Client) Close() error {
	if c.sqlDB != nil {
		return c.sqlDB.Close()
	}
	return nil
}

// Stats 获取连接池统计信息
func (c *Client) Stats() sql.DBStats {
	if c.sqlDB == nil {
		return sql.DBStats{}
	}
	return c.sqlDB.Stats()
}

// IsHealthy 返回健康状态
func (c *Client) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Ping(ctx) == nil
}

// gormLogWriter 适配 kit/log 到 GORM logger.Writer
type gormLogWriter struct {
	logger *log.Logger
}

func newGormLogWriter(l *log.Logger) *gormLogWriter {
	return &gormLogWriter{logger: l}
}

func (w *gormLogWriter) Printf(format string, args ...any) {
	if w.logger != nil {
		w.logger.Info().Msgf(format, args...)
	}
}
