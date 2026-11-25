package sqlite_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	kitgorm "github.com/kochabx/kit/x/store/gorm"
	"github.com/kochabx/kit/x/store/gorm/sqlite"
)

type item struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"not null"`
}

func TestConfigValidateAndBuildDSN(t *testing.T) {
	t.Parallel()

	if err := (sqlite.Config{}).Validate(); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := (sqlite.Config{
		Path:    ":memory:",
		Pragmas: map[string]string{"foreign_keys;DROP TABLE x": "1"},
	}).Validate(); err == nil {
		t.Fatal("expected error for unsafe pragma key")
	}

	dsn := sqlite.BuildDSN(sqlite.Config{
		Path: ":memory:",
		Pragmas: map[string]string{
			"busy_timeout": "5000",
			"foreign_keys": "ON",
		},
	})
	want := ":memory:?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%28ON%29"
	if dsn != want {
		t.Fatalf("BuildDSN() = %q, want %q", dsn, want)
	}
}

func TestIsolatedInMemoryAndMaxIdleClamping(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := sqlite.Config{
		Gorm: kitgorm.Config{
			EnableTracing: new(false),
			Pool:          kitgorm.PoolConfig{MaxIdleConns: 5},
		},
		Path:    ":memory:",
		Pragmas: map[string]string{"foreign_keys": "ON"},
	}
	c1, err := sqlite.New(cfg)
	if err != nil {
		t.Fatalf("c1 New failed (MaxIdleConns should clamp cleanly): %v", err)
	}
	defer func() { _ = c1.Close() }()

	c2, err := sqlite.New(cfg)
	if err != nil {
		t.Fatalf("c2 New failed: %v", err)
	}
	defer func() { _ = c2.Close() }()

	if err := c1.DB().WithContext(ctx).AutoMigrate(&item{}); err != nil {
		t.Fatalf("c1 AutoMigrate failed: %v", err)
	}
	if err := c1.DB().WithContext(ctx).Create(&item{Name: "isolated"}).Error; err != nil {
		t.Fatalf("c1 Create failed: %v", err)
	}
	var count int64
	if err := c2.DB().WithContext(ctx).Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='items'").Scan(&count).Error; err != nil {
		t.Fatalf("c2 table check failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected isolated in-memory databases, got shared table count=%d", count)
	}
}

func TestInMemoryCRUDLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, err := sqlite.New(sqlite.Config{
		Gorm: kitgorm.Config{EnableTracing: new(false)},
		Path: ":memory:",
		Pragmas: map[string]string{
			"foreign_keys": "ON",
			"busy_timeout": "3000",
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("deferred Close failed: %v", err)
		}
	}()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	db := client.DB().WithContext(ctx)
	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	rec := item{Name: "alpha"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if rec.ID == 0 {
		t.Fatal("expected non-zero ID after insert")
	}

	var loaded item
	if err := db.First(&loaded, rec.ID).Error; err != nil {
		t.Fatalf("First failed: %v", err)
	}
	if loaded.Name != "alpha" {
		t.Fatalf("loaded.Name = %q, want alpha", loaded.Name)
	}

	if err := db.Model(&loaded).Update("name", "beta").Error; err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	var updated item
	if err := db.First(&updated, rec.ID).Error; err != nil {
		t.Fatalf("First after Update failed: %v", err)
	}
	if updated.Name != "beta" {
		t.Fatalf("updated.Name = %q, want beta", updated.Name)
	}

	if err := db.Delete(&updated).Error; err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	var count int64
	if err := db.Model(&item{}).Count(&count).Error; err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}

	if err := client.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("idempotent Close failed: %v", err)
	}
}

func TestPrivateMemoryPoolConstraintsAndParamsFilter(t *testing.T) {
	t.Parallel()

	// Reject pool settings that allow >1 connection or recycle the sole :memory: connection.
	for _, tc := range []struct {
		name string
		cfg  sqlite.Config
	}{
		{
			name: ":memory: max_open_conns > 1",
			cfg:  sqlite.Config{Path: ":memory:", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{MaxOpenConns: 2}}},
		},
		{
			name: ":memory: conn_max_lifetime > 0",
			cfg:  sqlite.Config{Path: ":memory:", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{ConnMaxLifetime: time.Minute}}},
		},
		{
			name: ":memory: conn_max_idle_time > 0",
			cfg:  sqlite.Config{Path: ":memory:", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{ConnMaxIdleTime: time.Minute}}},
		},
		{
			name: "file::memory: conn_max_idle_time > 0",
			cfg:  sqlite.Config{Path: "file::memory:?mode=memory", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{ConnMaxIdleTime: time.Second}}},
		},
		{
			name: "file:review private memory max_open_conns > 1",
			cfg:  sqlite.Config{Path: "file:review?mode=memory&cache=private", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{MaxOpenConns: 2}}},
		},
		{
			name: "file:review private memory conn_max_lifetime > 0",
			cfg:  sqlite.Config{Path: "file:review?mode=memory&cache=private", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{ConnMaxLifetime: time.Second}}},
		},
		{
			name: "file:review private memory conn_max_idle_time > 0",
			cfg:  sqlite.Config{Path: "file:review?mode=memory&cache=private", Gorm: kitgorm.Config{Pool: kitgorm.PoolConfig{ConnMaxIdleTime: time.Second}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
			if c, err := sqlite.New(tc.cfg); err == nil {
				_ = c.Close()
				t.Fatalf("expected New error for %s", tc.name)
			}
		})
	}

	// Default :memory: pins MaxOpenConnections=1, disables idle/lifetime eviction, and redacts bound params in logs.
	type uniqueItem struct {
		ID    uint   `gorm:"primaryKey"`
		Token string `gorm:"uniqueIndex;not null"`
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, err := sqlite.New(sqlite.Config{
		Gorm: kitgorm.Config{
			EnableTracing: new(false),
			SlowThreshold: time.Nanosecond,
		},
		Path: ":memory:",
	}, kitgorm.WithLogger(logger))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	if stats := client.SQLDB().Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
	}
	db := client.DB().WithContext(t.Context())
	if err := db.AutoMigrate(&uniqueItem{}); err != nil {
		t.Fatal(err)
	}
	slowSecret := "SLOW-SECRET-MARKER-123"
	if err := db.Create(&uniqueItem{Token: slowSecret}).Error; err != nil {
		t.Fatal(err)
	}
	// Duplicate token triggers ERROR query log; slow threshold 1ns already triggered WARN slow log.
	errSecret := "ERR-SECRET-MARKER-456"
	if err := db.Create(&uniqueItem{Token: slowSecret}).Error; err == nil {
		t.Fatal("expected unique constraint error")
	}
	if err := db.Where("token = ?", errSecret).First(&uniqueItem{}).Error; err == nil {
		t.Fatal("expected not found")
	}

	var count int64
	if err := db.Model(&uniqueItem{}).Where("token = ?", slowSecret).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected row preserved across queries, count=%d err=%v", count, err)
	}
	if stats := client.SQLDB().Stats(); stats.MaxIdleClosed != 0 || stats.MaxLifetimeClosed != 0 {
		t.Fatalf("expected zero idle/lifetime connection closes, got %+v", stats)
	}
	out := logBuf.String()
	for _, secret := range []string{slowSecret, errSecret} {
		if strings.Contains(out, secret) {
			t.Fatalf("bound SQL parameter %q leaked into GORM logs: %s", secret, out)
		}
	}

	// Named private memory URI ("file:review?mode=memory&cache=private") also pins to 1 connection cleanly.
	namedMem, err := sqlite.New(sqlite.Config{
		Gorm: kitgorm.Config{EnableTracing: new(false)},
		Path: "file:review?mode=memory&cache=private",
	})
	if err != nil {
		t.Fatalf("named private memory New failed: %v", err)
	}
	defer func() { _ = namedMem.Close() }()
	if stats := namedMem.SQLDB().Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("named private memory MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
	}
	if err := namedMem.DB().WithContext(t.Context()).AutoMigrate(&item{}); err != nil {
		t.Fatal(err)
	}
	if err := namedMem.DB().WithContext(t.Context()).Create(&item{Name: "named-mem"}).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			var got item
			errs <- namedMem.DB().WithContext(t.Context()).First(&got, "name = ?", "named-mem").Error
		})
	}
	wg.Wait()
	close(errs)
	for qErr := range errs {
		if qErr != nil {
			t.Fatalf("concurrent query on named private memory URI failed: %v", qErr)
		}
	}
}
