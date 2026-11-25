package gorm

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	gormlib "gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "zero value defaults", cfg: Config{}},
		{
			name: "valid pool",
			cfg: Config{
				Pool: PoolConfig{
					MaxIdleConns:    5,
					MaxOpenConns:    20,
					ConnMaxLifetime: time.Minute,
					ConnMaxIdleTime: time.Second,
				},
				SlowThreshold: 100 * time.Millisecond,
			},
		},
		{
			name:    "idle exceeds open",
			cfg:     Config{Pool: PoolConfig{MaxIdleConns: 20, MaxOpenConns: 10}},
			wantErr: true,
		},
		{
			name:    "negative slow threshold",
			cfg:     Config{SlowThreshold: -time.Millisecond},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewRequiresDialector(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Config{}); err == nil {
		t.Fatal("expected error for nil dialector")
	}
}

func TestSlogLoggerTrace(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sl := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	l := newSlogLogger(sl, 10*time.Millisecond, false)
	ctx := t.Context()

	// ErrRecordNotFound ignored in Warn mode when logRecordNotFound == false.
	l.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 0 }, gormlib.ErrRecordNotFound)
	if buf.Len() != 0 {
		t.Fatalf("expected ErrRecordNotFound not logged, got %q", buf.String())
	}

	// In Info mode, SQL is logged at DEBUG level, never ERROR for ErrRecordNotFound.
	l.LogMode(gormlogger.Info).Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 0 }, gormlib.ErrRecordNotFound)
	if !strings.Contains(buf.String(), "level=DEBUG") || strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("expected DEBUG SQL log without ERROR, got %q", buf.String())
	}

	// Non-RecordNotFound error is logged as ERROR.
	l.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT bad", 0 }, errors.New("syntax error"))
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "syntax error") {
		t.Fatalf("expected ERROR log with cause, got %q", buf.String())
	}

	// Slow query logged as WARN when error is nil.
	buf.Reset()
	l.Trace(ctx, time.Now().Add(-50*time.Millisecond), func() (string, int64) { return "SELECT slow", 1 }, nil)
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "slow gorm query") {
		t.Fatalf("expected WARN slow query log, got %q", buf.String())
	}
}
