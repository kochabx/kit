package mysql

import (
	"cmp"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net"
	"strconv"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/plugin/opentelemetry/tracing"

	kitgorm "github.com/kochabx/kit/x/store/gorm"
)

const (
	defaultPort         = 3306
	defaultCharset      = "utf8mb4"
	defaultDialTimeout  = 5 * time.Second
	defaultReadTimeout  = 30 * time.Second
	defaultWriteTimeout = 30 * time.Second
)

// Config configures a GORM MySQL client.
type Config struct {
	Gorm            kitgorm.Config    `json:"gorm"`
	DSN             string            `json:"dsn"`
	Host            string            `json:"host"`
	Port            int               `json:"port"`
	UnixSocket      string            `json:"unix_socket"`
	Database        string            `json:"database"`
	Username        string            `json:"username"`
	Password        string            `json:"password"`
	Charset         string            `json:"charset"`
	Collation       string            `json:"collation"`
	ParseTime       *bool             `json:"parse_time"`
	Loc             *time.Location    `json:"-"`
	TLS             *tls.Config       `json:"-"`
	TLSConfigName   string            `json:"tls_config_name"`
	Timeout         time.Duration     `json:"timeout"`
	ReadTimeout     time.Duration     `json:"read_timeout"`
	WriteTimeout    time.Duration     `json:"write_timeout"`
	MultiStatements bool              `json:"multi_statements"`
	Params          map[string]string `json:"params"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if err := c.Gorm.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.DSN == "" {
		if c.UnixSocket == "" && c.Host == "" {
			errs = append(errs, errors.New("gorm/mysql: host or unix_socket is required when dsn is empty"))
		}
		if c.Database == "" {
			errs = append(errs, errors.New("gorm/mysql: database is required when dsn is empty"))
		}
	}
	if c.Port < 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("gorm/mysql: port %d out of range", c.Port))
	}
	if c.Timeout < 0 {
		errs = append(errs, errors.New("gorm/mysql: timeout cannot be negative"))
	}
	if c.ReadTimeout < 0 {
		errs = append(errs, errors.New("gorm/mysql: read_timeout cannot be negative"))
	}
	if c.WriteTimeout < 0 {
		errs = append(errs, errors.New("gorm/mysql: write_timeout cannot be negative"))
	}
	return errors.Join(errs...)
}

// BuildMySQLConfig builds a structured *mysqldriver.Config supporting TCP, Unix sockets, and cloned TLS.
func BuildMySQLConfig(cfg Config) (*mysqldriver.Config, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var (
		mc  *mysqldriver.Config
		err error
	)
	if cfg.DSN != "" {
		mc, err = mysqldriver.ParseDSN(cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("gorm/mysql: parse dsn: %w", err)
		}
	} else {
		mc = mysqldriver.NewConfig()
		mc.User = cfg.Username
		mc.Passwd = cfg.Password
		mc.DBName = cfg.Database
		if cfg.UnixSocket != "" {
			mc.Net = "unix"
			mc.Addr = cfg.UnixSocket
		} else {
			port := cmp.Or(cfg.Port, defaultPort)
			mc.Net = "tcp"
			mc.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(port))
		}
		mc.ParseTime = cfg.ParseTime == nil || *cfg.ParseTime
		mc.MultiStatements = cfg.MultiStatements
		mc.Collation = cfg.Collation
		if cfg.Loc != nil {
			mc.Loc = cfg.Loc
		}
		charset := cmp.Or(cfg.Charset, cfg.Params["charset"], defaultCharset)
		if err := mc.Apply(mysqldriver.Charset(charset, cfg.Collation)); err != nil {
			return nil, fmt.Errorf("gorm/mysql: apply charset: %w", err)
		}
		if len(cfg.Params) > 0 {
			mc.Params = make(map[string]string, len(cfg.Params))
			maps.Copy(mc.Params, cfg.Params)
			delete(mc.Params, "charset")
		}
	}

	if cfg.TLSConfigName != "" {
		mc.TLSConfig = cfg.TLSConfigName
		mc.TLS = nil
		mc.AllowFallbackToPlaintext = false
	}
	if cfg.TLS != nil {
		mc.TLS = cfg.TLS.Clone()
		mc.TLSConfig = ""
		mc.AllowFallbackToPlaintext = false
	}
	if cfg.Timeout > 0 {
		mc.Timeout = cfg.Timeout
	} else if mc.Timeout == 0 {
		mc.Timeout = defaultDialTimeout
	}
	if cfg.ReadTimeout > 0 {
		mc.ReadTimeout = cfg.ReadTimeout
	} else if mc.ReadTimeout == 0 {
		mc.ReadTimeout = defaultReadTimeout
	}
	if cfg.WriteTimeout > 0 {
		mc.WriteTimeout = cfg.WriteTimeout
	} else if mc.WriteTimeout == 0 {
		mc.WriteTimeout = defaultWriteTimeout
	}
	return mc, nil
}

// New creates a lazy GORM MySQL client without establishing network connections.
func New(cfg Config, opts ...kitgorm.Option) (*kitgorm.Client, error) {
	mc, err := BuildMySQLConfig(cfg)
	if err != nil {
		return nil, err
	}

	connector, err := mysqldriver.NewConnector(mc)
	if err != nil {
		return nil, fmt.Errorf("gorm/mysql: create connector: %w", err)
	}
	sqlDB := sql.OpenDB(connector)
	dialector := gormmysql.New(gormmysql.Config{
		Conn:                      sqlDB,
		DSNConfig:                 mc,
		SkipInitializeWithVersion: true,
	})

	allOpts := make([]kitgorm.Option, 0, len(opts)+1)
	allOpts = append(allOpts, kitgorm.WithTracingOptions(tracing.WithDBSystem("mysql")))
	allOpts = append(allOpts, opts...)
	client, err := kitgorm.New(dialector, cfg.Gorm, allOpts...)
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return client, nil
}
