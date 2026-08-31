package db

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// SQLiteConfig contains SQLite connection settings.
type SQLiteConfig struct {
	Path string `json:"path" validate:"required"`

	JournalMode string        `json:"journalMode" default:"WAL" validate:"required"`
	CacheSize   int           `json:"cacheSize" default:"-2000"`
	BusyTimeout time.Duration `json:"busyTimeout" default:"5s" validate:"gte=0"`
	SyncMode    string        `json:"syncMode" default:"NORMAL" validate:"required"`
	ForeignKeys bool          `json:"foreignKeys"`
}

func (cfg SQLiteConfig) dialector() (string, gorm.Dialector, error) {
	cfg, err := resolveDriverConfig(driverSQLite, cfg)
	if err != nil {
		return "", nil, err
	}
	query := make(url.Values)
	query.Set("_journal_mode", cfg.JournalMode)
	query.Set("_cache_size", strconv.Itoa(cfg.CacheSize))
	query.Set("_busy_timeout", strconv.FormatInt(cfg.BusyTimeout.Milliseconds(), 10))
	query.Set("_synchronous", cfg.SyncMode)
	query.Set("_foreign_keys", strconv.FormatBool(cfg.ForeignKeys))

	path := cfg.Path
	if path == ":memory:" {
		path = "file::memory:"
	} else if !strings.HasPrefix(path, "file:") {
		path = "file:" + path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return driverSQLite, sqlite.Open(path + separator + query.Encode()), nil
}
