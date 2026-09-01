package db

import (
	"context"
	"database/sql"
	"fmt"
	stdlog "log"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	kitlog "github.com/kochabx/kit/log"
)

// Client owns a GORM database and its underlying connection pool.
type Client struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

// New creates a database client.
func New(cfg Config, opts ...Option) (*Client, error) {
	if isNilDriver(cfg.Driver) {
		return nil, fmt.Errorf("%w: driver is required", ErrInvalidConfig)
	}

	driverName, dialector, err := cfg.Driver.dialector()
	if err != nil {
		return nil, err
	}
	cfg, err = resolveConfig(cfg, driverName)
	if err != nil {
		return nil, err
	}

	settings, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	return openDatabase(driverName, dialector, cfg, settings)
}

func resolveOptions(opts []Option) (openOptions, error) {
	settings := openOptions{}
	for index, option := range opts {
		if option == nil {
			return openOptions{}, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := option(&settings); err != nil {
			return openOptions{}, err
		}
	}
	return settings, nil
}

func openDatabase(driver string, dialector gorm.Dialector, cfg Config, settings openOptions) (*Client, error) {
	gormDB, err := gorm.Open(dialector, newGORMConfig(cfg, settings.logger))
	if err != nil {
		closeGORMDB(gormDB)
		return nil, fmt.Errorf("db: open %s: %w", driver, err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		closeGORMDB(gormDB)
		return nil, fmt.Errorf("db: get %s connection pool: %w", driver, err)
	}
	applyPoolConfig(sqlDB, *cfg.Pool)

	if err := installPlugins(gormDB, settings.plugins); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: install %s plugin: %w", driver, err)
	}
	return &Client{db: gormDB, sqlDB: sqlDB}, nil
}

func installPlugins(db *gorm.DB, plugins []gorm.Plugin) error {
	for _, plugin := range plugins {
		if err := db.Use(plugin); err != nil {
			return fmt.Errorf("%q: %w", plugin.Name(), err)
		}
	}
	return nil
}

func isNilDriver(driver DriverConfig) bool {
	if driver == nil {
		return true
	}
	value := reflect.ValueOf(driver)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func closeGORMDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func newGORMConfig(cfg Config, output *kitlog.Logger) *gorm.Config {
	result := &gorm.Config{}
	if cfg.GORM != nil {
		*result = *cfg.GORM
	}
	if result.Logger != nil {
		return result
	}
	if output == nil {
		result.Logger = logger.Discard
	} else {
		result.Logger = logger.New(
			stdlog.New(output, "", 0),
			logger.Config{
				SlowThreshold:             cfg.Log.SlowQueryThreshold,
				LogLevel:                  cfg.Log.Level,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		)
	}
	return result
}

func applyPoolConfig(db *sql.DB, pool PoolConfig) {
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
}

func (c *Client) DB() *gorm.DB       { return c.db }
func (c *Client) Stats() sql.DBStats { return c.sqlDB.Stats() }

func (c *Client) Ping(ctx context.Context) error {
	if err := c.sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}

func (c *Client) Close() error {
	if err := c.sqlDB.Close(); err != nil {
		return fmt.Errorf("db: close: %w", err)
	}
	return nil
}

func (c *Client) Start(ctx context.Context) error       { return c.Ping(ctx) }
func (c *Client) Stop(context.Context) error            { return c.Close() }
func (c *Client) HealthCheck(ctx context.Context) error { return c.Ping(ctx) }
