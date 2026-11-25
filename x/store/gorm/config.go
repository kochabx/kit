package gorm

import (
	"errors"
	"time"
)

const (
	defaultMaxIdleConns    = 10
	defaultMaxOpenConns    = 100
	defaultConnMaxLifetime = time.Hour
	defaultConnMaxIdleTime = 10 * time.Minute
	defaultSlowThreshold   = 200 * time.Millisecond
)

// PoolConfig configures the underlying *sql.DB connection pool.
type PoolConfig struct {
	MaxIdleConns    int           `json:"max_idle_conns"`
	MaxOpenConns    int           `json:"max_open_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`
}

// Config configures a GORM Client.
type Config struct {
	Pool                                     PoolConfig    `json:"pool"`
	SlowThreshold                            time.Duration `json:"slow_threshold"`
	LogRecordNotFound                        bool          `json:"log_record_not_found"`
	EnableTracing                            *bool         `json:"enable_tracing"`
	SkipDefaultTransaction                   bool          `json:"skip_default_transaction"`
	PrepareStmt                              bool          `json:"prepare_stmt"`
	DisableForeignKeyConstraintWhenMigrating bool          `json:"disable_foreign_key_constraint_when_migrating"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if c.Pool.MaxIdleConns < 0 {
		errs = append(errs, errors.New("gorm: pool.max_idle_conns cannot be negative"))
	}
	if c.Pool.MaxOpenConns < 0 {
		errs = append(errs, errors.New("gorm: pool.max_open_conns cannot be negative"))
	}
	if c.Pool.MaxOpenConns > 0 && c.Pool.MaxIdleConns > c.Pool.MaxOpenConns {
		errs = append(errs, errors.New("gorm: pool.max_idle_conns cannot exceed pool.max_open_conns"))
	}
	if c.Pool.ConnMaxLifetime < 0 {
		errs = append(errs, errors.New("gorm: pool.conn_max_lifetime cannot be negative"))
	}
	if c.Pool.ConnMaxIdleTime < 0 {
		errs = append(errs, errors.New("gorm: pool.conn_max_idle_time cannot be negative"))
	}
	if c.SlowThreshold < 0 {
		errs = append(errs, errors.New("gorm: slow_threshold cannot be negative"))
	}
	return errors.Join(errs...)
}
