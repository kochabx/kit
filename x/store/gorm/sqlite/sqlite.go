package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/plugin/opentelemetry/tracing"
	_ "modernc.org/sqlite"

	kitgorm "github.com/kochabx/kit/x/store/gorm"
)

// Config configures a pure-Go CGO-free SQLite GORM client backed by modernc.org/sqlite.
type Config struct {
	Gorm    kitgorm.Config    `json:"gorm"`
	Path    string            `json:"path"`
	Pragmas map[string]string `json:"pragmas"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if err := c.Gorm.Validate(); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(c.Path) == "" {
		errs = append(errs, errors.New("gorm/sqlite: path is required"))
	}
	if isPrivateMemoryPath(c.Path) {
		if c.Gorm.Pool.MaxOpenConns > 1 {
			errs = append(errs, errors.New("gorm/sqlite: private :memory: database requires pool.max_open_conns <= 1"))
		}
		if c.Gorm.Pool.ConnMaxLifetime > 0 {
			errs = append(errs, errors.New("gorm/sqlite: private :memory: database cannot set pool.conn_max_lifetime"))
		}
		if c.Gorm.Pool.ConnMaxIdleTime > 0 {
			errs = append(errs, errors.New("gorm/sqlite: private :memory: database cannot set pool.conn_max_idle_time"))
		}
	}
	for k, v := range c.Pragmas {
		if k == "" || strings.ContainsAny(k, ";() \t\r\n") {
			errs = append(errs, fmt.Errorf("gorm/sqlite: invalid pragma key %q", k))
		}
		if v == "" || strings.ContainsAny(v, ";\r\n") {
			errs = append(errs, fmt.Errorf("gorm/sqlite: invalid pragma value for %q", k))
		}
	}
	return errors.Join(errs...)
}

// BuildDSN builds a modernc.org/sqlite DSN from cfg.
func BuildDSN(cfg Config) string {
	if len(cfg.Pragmas) == 0 {
		return cfg.Path
	}
	base := cfg.Path

	keys := make([]string, 0, len(cfg.Pragmas))
	for k := range cfg.Pragmas {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	q := make(url.Values, len(keys))
	for _, k := range keys {
		q.Add("_pragma", k+"("+cfg.Pragmas[k]+")")
	}

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + q.Encode()
}

func isPrivateMemoryPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == ":memory:" {
		return true
	}
	if qStr, ok := strings.CutPrefix(trimmed, ":memory:?"); ok {
		q, err := url.ParseQuery(qStr)
		return err == nil && !strings.EqualFold(q.Get("cache"), "shared")
	}
	u, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return false
	}
	q := u.Query()
	isMem := u.Opaque == ":memory:" || u.Path == ":memory:" || strings.EqualFold(q.Get("mode"), "memory")
	return isMem && !strings.EqualFold(q.Get("cache"), "shared")
}

// New creates a pure-Go GORM SQLite client without requiring CGO.
func New(cfg Config, opts ...kitgorm.Option) (*kitgorm.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	isMem := isPrivateMemoryPath(cfg.Path)
	if isMem {
		// Private in-memory SQLite databases live on a single connection; pin open/idle to 1 and disable expiry.
		cfg.Gorm.Pool.MaxOpenConns = 1
		cfg.Gorm.Pool.MaxIdleConns = 1
	}

	sqlDB, err := sql.Open("sqlite", BuildDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("gorm/sqlite: open sql db: %w", err)
	}

	dialector := gormsqlite.Dialector{
		DriverName: "sqlite",
		Conn:       sqlDB,
	}

	allOpts := make([]kitgorm.Option, 0, len(opts)+1)
	allOpts = append(allOpts, kitgorm.WithTracingOptions(tracing.WithDBSystem("sqlite")))
	allOpts = append(allOpts, opts...)
	client, err := kitgorm.New(dialector, cfg.Gorm, allOpts...)
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if isMem {
		client.SQLDB().SetConnMaxLifetime(0)
		client.SQLDB().SetConnMaxIdleTime(0)
	}
	return client, nil
}
