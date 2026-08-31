package db

import (
	"math"
	"net"
	"net/url"
	"strconv"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgresConfig contains PostgreSQL connection and dialector settings.
type PostgresConfig struct {
	Host     string `json:"host" default:"127.0.0.1" validate:"required"`
	Port     int    `json:"port" default:"5432" validate:"gte=1,lte=65535"`
	User     string `json:"user" default:"postgres" validate:"required"`
	Password string `json:"password"`
	Database string `json:"database" validate:"required"`

	SSLMode              string        `json:"sslMode" default:"disable" validate:"required"`
	TimeZone             string        `json:"timeZone" default:"Asia/Shanghai" validate:"required"`
	ConnectTimeout       time.Duration `json:"connectTimeout" default:"10s" validate:"gte=0"`
	PreferSimpleProtocol bool          `json:"preferSimpleProtocol"`
}

func (cfg PostgresConfig) dialector() (string, gorm.Dialector, error) {
	cfg, err := resolveDriverConfig(driverPostgres, cfg)
	if err != nil {
		return "", nil, err
	}
	query := make(url.Values)
	query.Set("sslmode", cfg.SSLMode)
	query.Set("TimeZone", cfg.TimeZone)
	query.Set("connect_timeout", strconv.Itoa(int(math.Ceil(cfg.ConnectTimeout.Seconds()))))
	dsn := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     cfg.Database,
		RawQuery: query.Encode(),
	}).String()

	return driverPostgres, postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: cfg.PreferSimpleProtocol,
	}), nil
}
