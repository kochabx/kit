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
	MaxIdleConns    int           `json:"maxIdleConns" default:"10" validate:"gte=0"`
	MaxOpenConns    int           `json:"maxOpenConns" default:"100" validate:"gte=0"`
	ConnMaxLifetime time.Duration `json:"connMaxLifetime" default:"1h" validate:"gte=0"`
	ConnMaxIdleTime time.Duration `json:"connMaxIdleTime" default:"10m" validate:"gte=0"`
}

// LogConfig controls GORM log filtering and slow-query reporting.
type LogConfig struct {
	Level              logger.LogLevel `json:"level" default:"4" validate:"oneof=1 2 3 4"`
	SlowQueryThreshold time.Duration   `json:"slowQueryThreshold" validate:"gte=0"`
}

// Config contains settings shared by every database driver.
type Config struct {
	Driver DriverConfig `json:"-" validate:"-"`
	GORM   *gorm.Config `json:"-" validate:"-"`
	Pool   *PoolConfig  `json:"pool,omitempty"`
	Log    *LogConfig   `json:"log,omitempty"`
}

func resolveConfig(cfg Config, driver string) (Config, error) {
	pool, err := defaultPool(driver)
	if err != nil {
		return Config{}, err
	}
	if cfg.Pool != nil {
		pool = *cfg.Pool
	}
	cfg.Pool = &pool

	logConfig := LogConfig{}
	if cfg.Log != nil {
		logConfig = *cfg.Log
	}
	if err := defaults.Apply(&logConfig); err != nil {
		return Config{}, fmt.Errorf("%w: apply log defaults: %w", ErrInvalidConfig, err)
	}
	cfg.Log = &logConfig

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

func defaultPool(driver string) (PoolConfig, error) {
	if driver == driverSQLite {
		return PoolConfig{MaxIdleConns: 1, MaxOpenConns: 1}, nil
	}

	var pool PoolConfig
	if err := defaults.Apply(&pool); err != nil {
		return PoolConfig{}, fmt.Errorf("%w: apply pool defaults: %w", ErrInvalidConfig, err)
	}
	return pool, nil
}
