package db

import (
	"fmt"
	"net"
	"strconv"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MySQLConfig contains MySQL connection and dialector settings.
type MySQLConfig struct {
	Host     string `json:"host" default:"127.0.0.1" validate:"required"`
	Port     int    `json:"port" default:"3306" validate:"gte=1,lte=65535"`
	User     string `json:"user" default:"root" validate:"required"`
	Password string `json:"password"`
	Database string `json:"database" validate:"required"`

	Network      string        `json:"network" default:"tcp" validate:"required"`
	Charset      string        `json:"charset" default:"utf8mb4" validate:"required"`
	Collation    string        `json:"collation"`
	ParseTime    bool          `json:"parseTime"`
	Location     string        `json:"location" default:"Local" validate:"required"`
	Timeout      time.Duration `json:"timeout" default:"10s" validate:"gte=0"`
	ReadTimeout  time.Duration `json:"readTimeout" validate:"gte=0"`
	WriteTimeout time.Duration `json:"writeTimeout" validate:"gte=0"`

	SkipInitializeWithVersion bool `json:"skipInitializeWithVersion"`
}

func (cfg MySQLConfig) dialector() (string, gorm.Dialector, error) {
	cfg, err := resolveDriverConfig(driverMySQL, cfg)
	if err != nil {
		return "", nil, err
	}
	dsn := mysqldriver.NewConfig()
	dsn.User = cfg.User
	dsn.Passwd = cfg.Password
	dsn.Net = cfg.Network
	dsn.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dsn.DBName = cfg.Database
	dsn.Collation = cfg.Collation
	dsn.ParseTime = cfg.ParseTime
	dsn.Timeout = cfg.Timeout
	dsn.ReadTimeout = cfg.ReadTimeout
	dsn.WriteTimeout = cfg.WriteTimeout
	if cfg.Charset != "" {
		dsn.Params = map[string]string{"charset": cfg.Charset}
	}
	if cfg.Location != "" {
		location, err := time.LoadLocation(cfg.Location)
		if err != nil {
			return "", nil, fmt.Errorf("%w: mysql location %q: %w", ErrInvalidConfig, cfg.Location, err)
		}
		dsn.Loc = location
	}

	return driverMySQL, gormmysql.New(gormmysql.Config{
		DSNConfig:                 dsn,
		SkipInitializeWithVersion: cfg.SkipInitializeWithVersion,
	}), nil
}
