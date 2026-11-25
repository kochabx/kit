package pg

import (
	"cmp"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultPort              = 5432
	defaultSSLMode           = "disable"
	defaultMaxConns          = int32(25)
	defaultMinConns          = int32(2)
	defaultMaxConnLifetime   = time.Hour
	defaultMaxConnIdleTime   = 30 * time.Minute
	defaultHealthCheckPeriod = time.Minute
	defaultConnectTimeout    = 5 * time.Second
	defaultSlowThreshold     = 200 * time.Millisecond
)

// Config configures a PostgreSQL pgx connection pool.
type Config struct {
	DSN                string            `json:"dsn"`
	Host               string            `json:"host"`
	Port               int               `json:"port"`
	Database           string            `json:"database"`
	Username           string            `json:"username"`
	Password           string            `json:"password"`
	SSLMode            string            `json:"ssl_mode"`
	TLS                *tls.Config       `json:"-"`
	MaxConns           int32             `json:"max_conns"`
	MinConns           int32             `json:"min_conns"`
	MaxConnLifetime    time.Duration     `json:"max_conn_lifetime"`
	MaxConnIdleTime    time.Duration     `json:"max_conn_idle_time"`
	HealthCheckPeriod  time.Duration     `json:"health_check_period"`
	ConnectTimeout     time.Duration     `json:"connect_timeout"`
	SlowQueryThreshold time.Duration     `json:"slow_query_threshold"`
	EnableTracing      *bool             `json:"enable_tracing"`
	RuntimeParams      map[string]string `json:"runtime_params"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if c.DSN == "" {
		if c.Host == "" {
			errs = append(errs, errors.New("pg: host is required when dsn is empty"))
		}
		if c.Database == "" {
			errs = append(errs, errors.New("pg: database is required when dsn is empty"))
		}
		if c.SSLMode != "" && !validSSLMode(c.SSLMode) {
			errs = append(errs, fmt.Errorf("pg: invalid ssl_mode %q", c.SSLMode))
		}
	}
	if c.Port < 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("pg: port %d out of range", c.Port))
	}
	if c.MaxConns < 0 {
		errs = append(errs, errors.New("pg: max_conns cannot be negative"))
	}
	if c.MinConns < 0 {
		errs = append(errs, errors.New("pg: min_conns cannot be negative"))
	}
	if c.MaxConns > 0 && c.MinConns > c.MaxConns {
		errs = append(errs, errors.New("pg: min_conns cannot exceed max_conns"))
	}
	if c.MaxConnLifetime < 0 {
		errs = append(errs, errors.New("pg: max_conn_lifetime cannot be negative"))
	}
	if c.MaxConnIdleTime < 0 {
		errs = append(errs, errors.New("pg: max_conn_idle_time cannot be negative"))
	}
	if c.HealthCheckPeriod < 0 {
		errs = append(errs, errors.New("pg: health_check_period cannot be negative"))
	}
	if c.ConnectTimeout < 0 {
		errs = append(errs, errors.New("pg: connect_timeout cannot be negative"))
	}
	if c.SlowQueryThreshold < 0 {
		errs = append(errs, errors.New("pg: slow_query_threshold cannot be negative"))
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

// BuildPoolConfig builds a *pgxpool.Config programmatically without DSN string interpolation.
func (c Config) BuildPoolConfig(logger *slog.Logger) (*pgxpool.Config, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	poolCfg, err := c.parseBasePoolConfig()
	if err != nil {
		return nil, err
	}
	if c.TLS != nil {
		poolCfg.ConnConfig.TLSConfig = c.TLS.Clone()
		poolCfg.ConnConfig.Fallbacks = nil
	}
	if len(c.RuntimeParams) > 0 {
		if poolCfg.ConnConfig.RuntimeParams == nil {
			poolCfg.ConnConfig.RuntimeParams = make(map[string]string, len(c.RuntimeParams))
		}
		maps.Copy(poolCfg.ConnConfig.RuntimeParams, c.RuntimeParams)
	}
	if err := c.applyPoolLimits(poolCfg); err != nil {
		return nil, err
	}
	c.attachTracers(poolCfg, logger)
	return poolCfg, nil
}

func (c Config) parseBasePoolConfig() (*pgxpool.Config, error) {
	if c.DSN != "" {
		poolCfg, err := pgxpool.ParseConfig(c.DSN)
		if err != nil {
			return nil, fmt.Errorf("pg: parse dsn: %w", err)
		}
		return poolCfg, nil
	}

	port := cmp.Or(c.Port, defaultPort)
	sslMode := cmp.Or(c.SSLMode, defaultSSLMode)
	var (
		poolCfg *pgxpool.Config
		err     error
	)
	if filepath.IsAbs(c.Host) {
		// Unix domain socket paths use sslmode=disable and avoid URL host encoding.
		poolCfg, err = pgxpool.ParseConfig("sslmode=disable")
		if err != nil {
			return nil, fmt.Errorf("pg: init config: %w", err)
		}
		poolCfg.ConnConfig.Host = c.Host
		poolCfg.ConnConfig.Port = uint16(port)
		poolCfg.ConnConfig.TLSConfig = nil
		poolCfg.ConnConfig.Fallbacks = nil
	} else {
		u := url.URL{
			Scheme:   "postgres",
			Host:     net.JoinHostPort(c.Host, strconv.Itoa(port)),
			Path:     "/" + url.PathEscape(c.Database),
			RawQuery: "sslmode=" + sslMode,
		}
		poolCfg, err = pgxpool.ParseConfig(u.String())
		if err != nil {
			return nil, fmt.Errorf("pg: init config: %w", err)
		}
	}
	// Assign credentials programmatically to prevent DSN parameter injection.
	poolCfg.ConnConfig.Database = c.Database
	poolCfg.ConnConfig.User = c.Username
	poolCfg.ConnConfig.Password = c.Password
	return poolCfg, nil
}

func (c Config) applyPoolLimits(poolCfg *pgxpool.Config) error {
	if c.DSN != "" {
		if c.ConnectTimeout > 0 {
			poolCfg.ConnConfig.ConnectTimeout = c.ConnectTimeout
		}
		if c.MaxConns > 0 {
			poolCfg.MaxConns = c.MaxConns
		}
		if c.MinConns > 0 {
			poolCfg.MinConns = c.MinConns
		}
		if c.MaxConnLifetime > 0 {
			poolCfg.MaxConnLifetime = c.MaxConnLifetime
		}
		if c.MaxConnIdleTime > 0 {
			poolCfg.MaxConnIdleTime = c.MaxConnIdleTime
		}
		if c.HealthCheckPeriod > 0 {
			poolCfg.HealthCheckPeriod = c.HealthCheckPeriod
		}
	} else {
		poolCfg.ConnConfig.ConnectTimeout = cmp.Or(c.ConnectTimeout, defaultConnectTimeout)
		poolCfg.MaxConns = cmp.Or(c.MaxConns, defaultMaxConns)
		switch {
		case c.MinConns > 0:
			poolCfg.MinConns = c.MinConns
		case c.MaxConns == 0, poolCfg.MaxConns >= defaultMinConns:
			poolCfg.MinConns = defaultMinConns
		default:
			poolCfg.MinConns = 0
		}
		poolCfg.MaxConnLifetime = cmp.Or(c.MaxConnLifetime, defaultMaxConnLifetime)
		poolCfg.MaxConnIdleTime = cmp.Or(c.MaxConnIdleTime, defaultMaxConnIdleTime)
		poolCfg.HealthCheckPeriod = cmp.Or(c.HealthCheckPeriod, defaultHealthCheckPeriod)
	}
	if poolCfg.MaxConns > 0 && poolCfg.MinConns > poolCfg.MaxConns {
		return fmt.Errorf("pg: min_conns (%d) cannot exceed max_conns (%d)", poolCfg.MinConns, poolCfg.MaxConns)
	}
	return nil
}

func (c Config) attachTracers(poolCfg *pgxpool.Config, logger *slog.Logger) {
	var tracers []pgx.QueryTracer
	if c.EnableTracing == nil || *c.EnableTracing {
		tracers = append(tracers, otelpgx.NewTracer())
	}
	if logger != nil {
		tracers = append(tracers, &slogQueryTracer{
			logger:        logger,
			slowThreshold: cmp.Or(c.SlowQueryThreshold, defaultSlowThreshold),
		})
	}
	switch len(tracers) {
	case 1:
		poolCfg.ConnConfig.Tracer = tracers[0]
	case 2:
		poolCfg.ConnConfig.Tracer = multitracer.New(tracers...)
	}
}
