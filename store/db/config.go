package db

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
)

const (
	driverMySQL    = "mysql"
	driverPostgres = "postgres"
	driverSQLite   = "sqlite"
)

// DriverConfig is the closed set of supported database configurations.
type DriverConfig interface {
	dialector() (driver string, dialector gorm.Dialector, err error)
}

// PoolConfig controls database/sql connection reuse. An omitted Pool uses
// package defaults; values in a provided Pool are passed through unchanged.
type PoolConfig struct {
	MaxIdleConns    int           `json:"maxIdleConns" validate:"gte=0"`
	MaxOpenConns    int           `json:"maxOpenConns" validate:"gte=0"`
	ConnMaxLifetime time.Duration `json:"connMaxLifetime" validate:"gte=0"`
	ConnMaxIdleTime time.Duration `json:"connMaxIdleTime" validate:"gte=0"`
}

// Config contains settings shared by every database driver.
type Config struct {
	Driver             DriverConfig    `json:"-" validate:"-"`
	Pool               *PoolConfig     `json:"pool,omitempty"`
	LogLevel           logger.LogLevel `json:"logLevel" validate:"oneof=1 2 3 4"`
	SlowQueryThreshold time.Duration   `json:"slowQueryThreshold" validate:"gte=0"`
	GORMConfig         *gorm.Config    `json:"-" validate:"-"`
}

func resolveConfig(cfg Config, driver string) (Config, error) {
	if cfg.LogLevel == 0 {
		cfg.LogLevel = logger.Info
	}

	pool := defaultPool(driver)
	if cfg.Pool != nil {
		pool = *cfg.Pool
	}
	cfg.Pool = &pool
	if err := validator.Validate.Struct(context.Background(), &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: validate config: %w", ErrInvalidConfig, err)
	}
	if pool.MaxOpenConns > 0 && pool.MaxIdleConns > pool.MaxOpenConns {
		return Config{}, fmt.Errorf("%w: max idle connections exceeds max open connections", ErrInvalidConfig)
	}
	return cfg, nil
}

func resolveDriverConfig[T any](driver string, cfg T) (T, error) {
	var zero T

	if err := defaults.Apply(&cfg); err != nil {
		return zero, fmt.Errorf("%w: apply %s defaults: %w", ErrInvalidConfig, driver, err)
	}
	if err := validator.Validate.Struct(context.Background(), &cfg); err != nil {
		return zero, fmt.Errorf("%w: validate %s config: %w", ErrInvalidConfig, driver, err)
	}
	return cfg, nil
}

func defaultPool(driver string) PoolConfig {
	if driver == driverSQLite {
		return PoolConfig{MaxIdleConns: 1, MaxOpenConns: 1}
	}
	return PoolConfig{
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 10 * time.Minute,
	}
}
