package postgres

import (
	"cmp"
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/plugin/opentelemetry/tracing"

	kitgorm "github.com/kochabx/kit/x/store/gorm"
)

const (
	defaultPort           = 5432
	defaultSSLMode        = "disable"
	defaultConnectTimeout = 5 * time.Second
)

// Config configures a GORM PostgreSQL client.
type Config struct {
	Gorm                 kitgorm.Config    `json:"gorm"`
	DSN                  string            `json:"dsn"`
	Host                 string            `json:"host"`
	Port                 int               `json:"port"`
	Database             string            `json:"database"`
	Username             string            `json:"username"`
	Password             string            `json:"password"`
	SSLMode              string            `json:"ssl_mode"`
	TLS                  *tls.Config       `json:"-"`
	ConnectTimeout       time.Duration     `json:"connect_timeout"`
	PreferSimpleProtocol bool              `json:"prefer_simple_protocol"`
	WithoutReturning     bool              `json:"without_returning"`
	RuntimeParams        map[string]string `json:"runtime_params"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if err := c.Gorm.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.DSN == "" {
		if c.Host == "" {
			errs = append(errs, errors.New("gorm/postgres: host is required when dsn is empty"))
		}
		if c.Database == "" {
			errs = append(errs, errors.New("gorm/postgres: database is required when dsn is empty"))
		}
		if c.SSLMode != "" && !validSSLMode(c.SSLMode) {
			errs = append(errs, fmt.Errorf("gorm/postgres: invalid ssl_mode %q", c.SSLMode))
		}
	}
	if c.Port < 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("gorm/postgres: port %d out of range", c.Port))
	}
	if c.ConnectTimeout < 0 {
		errs = append(errs, errors.New("gorm/postgres: connect_timeout cannot be negative"))
	}
	return errors.Join(errs...)
}

func validSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

// BuildConnConfig builds a *pgx.ConnConfig programmatically without DSN string interpolation.
func BuildConnConfig(cfg Config) (*pgx.ConnConfig, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var (
		connCfg *pgx.ConnConfig
		err     error
	)
	if cfg.DSN != "" {
		connCfg, err = pgx.ParseConfig(cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("gorm/postgres: parse dsn: %w", err)
		}
	} else {
		port := cmp.Or(cfg.Port, defaultPort)
		sslMode := cmp.Or(cfg.SSLMode, defaultSSLMode)
		if filepath.IsAbs(cfg.Host) {
			connCfg, err = pgx.ParseConfig("sslmode=disable")
			if err != nil {
				return nil, fmt.Errorf("gorm/postgres: init config: %w", err)
			}
			connCfg.Host = cfg.Host
			connCfg.Port = uint16(port)
			connCfg.TLSConfig = nil
			connCfg.Fallbacks = nil
		} else {
			u := url.URL{
				Scheme:   "postgres",
				Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(port)),
				Path:     "/" + url.PathEscape(cfg.Database),
				RawQuery: "sslmode=" + sslMode,
			}
			connCfg, err = pgx.ParseConfig(u.String())
			if err != nil {
				return nil, fmt.Errorf("gorm/postgres: init config: %w", err)
			}
		}
		// Assign credentials programmatically to prevent DSN parameter injection.
		connCfg.Database = cfg.Database
		connCfg.User = cfg.Username
		connCfg.Password = cfg.Password
	}

	if cfg.TLS != nil {
		connCfg.TLSConfig = cfg.TLS.Clone()
		connCfg.Fallbacks = nil
	}
	if len(cfg.RuntimeParams) > 0 {
		if connCfg.RuntimeParams == nil {
			connCfg.RuntimeParams = make(map[string]string, len(cfg.RuntimeParams))
		}
		maps.Copy(connCfg.RuntimeParams, cfg.RuntimeParams)
	}
	if cfg.PreferSimpleProtocol {
		connCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}
	connCfg.ConnectTimeout = cmp.Or(cfg.ConnectTimeout, defaultConnectTimeout)
	return connCfg, nil
}

// New creates a lazy GORM PostgreSQL client without establishing network connections.
func New(cfg Config, opts ...kitgorm.Option) (*kitgorm.Client, error) {
	connCfg, err := BuildConnConfig(cfg)
	if err != nil {
		return nil, err
	}

	sqlDB := stdlib.OpenDB(*connCfg)
	dialector := gormpostgres.New(gormpostgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: cfg.PreferSimpleProtocol,
		WithoutReturning:     cfg.WithoutReturning,
	})

	allOpts := make([]kitgorm.Option, 0, len(opts)+1)
	allOpts = append(allOpts, kitgorm.WithTracingOptions(tracing.WithDBSystem("postgresql")))
	allOpts = append(allOpts, opts...)
	client, err := kitgorm.New(dialector, cfg.Gorm, allOpts...)
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return client, nil
}
