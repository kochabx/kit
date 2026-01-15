package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrUnsupportedDriver  = errors.New("unsupported database driver")
	ErrInvalidConfig      = errors.New("invalid database configuration")
	ErrGormNotInitialized = errors.New("gorm not initialized")
)

// Gorm GORM 数据库连接包装器
type Gorm struct {
	config DriverConfig
	DB     *gorm.DB
}

// GormOption Gorm 配置选项函数类型
type GormOption func(*Gorm)

// NewGorm 创建新的 Gorm 实例
func NewGorm(config DriverConfig, opts ...GormOption) (*Gorm, error) {
	if config == nil {
		return nil, ErrInvalidConfig
	}

	g := &Gorm{
		config: config,
	}

	// 初始化驱动配置
	if err := config.Init(); err != nil {
		return nil, err
	}

	// 应用选项
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}

	// 创建数据库连接
	gormDB, err := g.createDB()
	if err != nil {
		return nil, err
	}
	g.DB = gormDB

	// 测试连接
	if err := g.Ping(context.TODO()); err != nil {
		// 如果连接失败，确保清理资源
		_ = g.Close()
		return nil, err
	}

	return g, nil
}

// createDB 创建数据库连接
func (g *Gorm) createDB() (*gorm.DB, error) {
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.LogLevel(g.config.LogLevel())),
	}
	dsn := g.config.Dsn()

	var db *gorm.DB
	var err error

	switch g.config.Driver() {
	case DriverMySQL:
		db, err = gorm.Open(mysql.Open(dsn), gormConfig)
	case DriverPostgreSQL:
		db, err = gorm.Open(postgres.Open(dsn), gormConfig)
	case DriverSQLite:
		db, err = gorm.Open(sqlite.Open(dsn), gormConfig)
	default:
		return nil, ErrUnsupportedDriver
	}

	if err != nil {
		return nil, err
	}

	// 配置连接池
	if err := g.setConnectionPool(db); err != nil {
		return nil, err
	}

	return db, nil
}

// setConnectionPool 配置数据库连接池
func (g *Gorm) setConnectionPool(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	connConfig := g.config.CloneConn()
	sqlDB.SetMaxIdleConns(connConfig.MaxIdleConns)
	sqlDB.SetMaxOpenConns(connConfig.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(connConfig.ConnMaxLifetime) * time.Second)

	return nil
}

// Ping 测试数据库连接是否正常
func (g *Gorm) Ping(ctx context.Context) error {
	if g.DB == nil {
		return ErrGormNotInitialized
	}

	sqlDB, err := g.DB.DB()
	if err != nil {
		return err
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}

	return nil
}

// Close 关闭数据库连接
func (g *Gorm) Close() error {
	if g.DB == nil {
		return nil
	}

	sqlDB, err := g.DB.DB()
	if err != nil {
		return err
	}

	if err := sqlDB.Close(); err != nil {
		return err
	}

	g.DB = nil // 清空引用，避免重复关闭
	return nil
}

// Stats 获取数据库连接池统计信息
func (g *Gorm) Stats() (sql.DBStats, error) {
	if g.DB == nil {
		return sql.DBStats{}, ErrGormNotInitialized
	}

	sqlDB, err := g.DB.DB()
	if err != nil {
		return sql.DBStats{}, err
	}

	return sqlDB.Stats(), nil
}

// GetDB 获取 GORM 数据库实例
func (g *Gorm) GetDB() *gorm.DB {
	return g.DB
}
