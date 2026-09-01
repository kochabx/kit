package db

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	kitlog "github.com/kochabx/kit/log"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	var nilSQLite *SQLiteConfig
	tests := []struct {
		name string
		cfg  Config
		err  error
	}{
		{name: "missing driver", cfg: Config{}, err: ErrInvalidConfig},
		{name: "typed nil driver", cfg: Config{Driver: nilSQLite}, err: ErrInvalidConfig},
		{name: "missing mysql database", cfg: Config{Driver: MySQLConfig{}}, err: ErrInvalidConfig},
		{name: "invalid mysql port", cfg: Config{Driver: MySQLConfig{Database: "app", Port: 70000}}, err: ErrInvalidConfig},
		{name: "missing postgres database", cfg: Config{Driver: PostgresConfig{}}, err: ErrInvalidConfig},
		{name: "missing sqlite path", cfg: Config{Driver: SQLiteConfig{}}, err: ErrInvalidConfig},
		{name: "invalid log level", cfg: Config{Driver: SQLiteConfig{Path: ":memory:"}, Log: &LogConfig{Level: logger.LogLevel(99)}}, err: ErrInvalidConfig},
		{name: "negative slow threshold", cfg: Config{Driver: SQLiteConfig{Path: ":memory:"}, Log: &LogConfig{SlowQueryThreshold: -1}}, err: ErrInvalidConfig},
		{name: "negative pool", cfg: Config{Driver: SQLiteConfig{Path: ":memory:"}, Pool: &PoolConfig{MaxOpenConns: -1}}, err: ErrInvalidConfig},
		{name: "idle exceeds open", cfg: Config{Driver: SQLiteConfig{Path: ":memory:"}, Pool: &PoolConfig{MaxIdleConns: 2, MaxOpenConns: 1}}, err: ErrInvalidConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestMySQLStructuredConfig(t *testing.T) {
	input := MySQLConfig{
		Host:        "db.example.com",
		Port:        3307,
		User:        "app@tenant",
		Password:    "p:a/ss@word",
		Database:    "service",
		Charset:     "utf8mb4",
		Collation:   "utf8mb4_unicode_ci",
		ParseTime:   true,
		Location:    "UTC",
		Timeout:     3 * time.Second,
		ReadTimeout: time.Second,
	}
	original := input
	name, dialector, err := input.dialector()
	require.NoError(t, err)
	assert.Equal(t, "mysql", name)
	mysqlDialect := dialector.(*gormmysql.Dialector)
	require.NotNil(t, mysqlDialect.DSNConfig)

	parsed, err := mysqldriver.ParseDSN(mysqlDialect.DSNConfig.FormatDSN())
	require.NoError(t, err)
	assert.Equal(t, input.User, parsed.User)
	assert.Equal(t, input.Password, parsed.Passwd)
	assert.Equal(t, input.Database, parsed.DBName)
	assert.Equal(t, "db.example.com:3307", parsed.Addr)
	assert.True(t, parsed.ParseTime)
	assert.Equal(t, time.UTC, parsed.Loc)
	assert.Equal(t, original, input, "dialector construction must not mutate input")
}

func TestPostgresStructuredConfig(t *testing.T) {
	input := PostgresConfig{
		Host:                 "2001:db8::1",
		User:                 "app@example.com",
		Password:             "p:a/ss word",
		Database:             "service",
		SSLMode:              "require",
		TimeZone:             "UTC",
		ConnectTimeout:       1500 * time.Millisecond,
		PreferSimpleProtocol: true,
	}
	name, dialector, err := input.dialector()
	require.NoError(t, err)
	assert.Equal(t, "postgres", name)
	postgresDialect := dialector.(*postgres.Dialector)
	parsed, err := url.Parse(postgresDialect.DSN)
	require.NoError(t, err)
	password, ok := parsed.User.Password()
	require.True(t, ok)
	assert.Equal(t, input.User, parsed.User.Username())
	assert.Equal(t, input.Password, password)
	assert.Equal(t, "[2001:db8::1]:5432", parsed.Host)
	assert.Equal(t, "/service", parsed.Path)
	assert.Equal(t, "require", parsed.Query().Get("sslmode"))
	assert.Equal(t, "2", parsed.Query().Get("connect_timeout"))
	assert.True(t, postgresDialect.PreferSimpleProtocol)
}

func TestSQLiteStructuredConfig(t *testing.T) {
	name, dialector, err := (SQLiteConfig{
		Path:        "data/app.db",
		JournalMode: "DELETE",
		CacheSize:   -1000,
		BusyTimeout: time.Second,
		SyncMode:    "FULL",
		ForeignKeys: true,
	}).dialector()
	require.NoError(t, err)
	assert.Equal(t, "sqlite", name)
	dsn := dialector.(*sqlite.Dialector).DSN
	assert.True(t, strings.HasPrefix(dsn, "file:data/app.db?"))
	assert.Contains(t, dsn, "_foreign_keys=true")
	assert.Contains(t, dsn, "_busy_timeout=1000")
}

func TestSQLiteLifecycleAndDefaultPool(t *testing.T) {
	cfg := Config{Driver: SQLiteConfig{Path: t.TempDir() + "/database.db"}}
	client, err := New(cfg)
	require.NoError(t, err)

	assert.NotNil(t, client.DB())
	assert.Equal(t, 1, client.Stats().MaxOpenConnections)
	require.NoError(t, client.Ping(t.Context()))
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
	require.Error(t, client.Ping(t.Context()))
	assert.Nil(t, cfg.Pool, "New must not mutate caller configuration")
}

func TestExplicitZeroPoolValues(t *testing.T) {
	pool := &PoolConfig{}
	client, err := New(Config{
		Driver: SQLiteConfig{Path: t.TempDir() + "/database.db"},
		Pool:   pool,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	assert.Zero(t, client.Stats().MaxOpenConnections)
	assert.Zero(t, *pool, "New must not mutate caller pool configuration")
}

func TestDefaultPoolReturnsIndependentValues(t *testing.T) {
	first, err := defaultPool("mysql")
	require.NoError(t, err)
	first.MaxOpenConns = 1
	second, err := defaultPool("mysql")
	require.NoError(t, err)
	assert.Equal(t, 100, second.MaxOpenConns)
	sqlitePool, err := defaultPool("sqlite")
	require.NoError(t, err)
	assert.Equal(t, 1, sqlitePool.MaxOpenConns)
}

func TestCustomGORMConfig(t *testing.T) {
	naming := schema.NamingStrategy{TablePrefix: "custom_"}
	gormConfig := &gorm.Config{SkipDefaultTransaction: true, NamingStrategy: naming}
	client, err := New(Config{
		Driver: SQLiteConfig{Path: ":memory:"},
		GORM:   gormConfig,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	assert.True(t, client.DB().Config.SkipDefaultTransaction)
	assert.Equal(t, "custom_widgets", client.DB().NamingStrategy.TableName("widget"))
	assert.False(t, gormConfig.DisableAutomaticPing, "New must not mutate GORM config")
}

type testGORMLogger struct{ logger.Interface }

func TestCustomGORMLoggerTakesPrecedence(t *testing.T) {
	custom := testGORMLogger{Interface: logger.Discard}
	client, err := New(Config{
		Driver: SQLiteConfig{Path: ":memory:"},
		GORM:   &gorm.Config{Logger: custom},
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	assert.Equal(t, custom, client.DB().Config.Logger)
}

func TestLoggingDisabledByDefault(t *testing.T) {
	client, err := New(Config{
		Driver: SQLiteConfig{Path: ":memory:"},
		Log:    &LogConfig{Level: logger.Info},
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	assert.Equal(t, logger.Discard, client.DB().Config.Logger)
}

func TestOptionsValidation(t *testing.T) {
	cfg := Config{Driver: SQLiteConfig{Path: ":memory:"}}
	_, err := New(cfg, nil)
	assert.ErrorIs(t, err, ErrInvalidOption)

	_, err = New(cfg, WithPlugins(nil))
	assert.ErrorIs(t, err, ErrInvalidOption)

	_, err = New(cfg, WithLogger(nil))
	assert.ErrorIs(t, err, ErrInvalidOption)
}

func TestWithLogger(t *testing.T) {
	client, err := New(Config{
		Driver: SQLiteConfig{Path: ":memory:"},
	}, WithLogger(kitlog.New()))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	assert.NotEqual(t, logger.Discard, client.DB().Config.Logger)
}

type failingPlugin struct{ err error }

func (failingPlugin) Name() string { return "failing" }

func (plugin failingPlugin) Initialize(*gorm.DB) error { return plugin.err }

type countingPlugin struct {
	name  string
	count *int
}

func (plugin countingPlugin) Name() string { return plugin.name }

func (plugin countingPlugin) Initialize(*gorm.DB) error {
	(*plugin.count)++
	return nil
}

func TestWithPluginsInstallsEachPluginOnce(t *testing.T) {
	firstCount, secondCount := 0, 0
	client, err := New(Config{Driver: SQLiteConfig{Path: ":memory:"}}, WithPlugins(
		countingPlugin{name: "first", count: &firstCount},
		countingPlugin{name: "second", count: &secondCount},
	))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	assert.Equal(t, 1, firstCount)
	assert.Equal(t, 1, secondCount)
}

func TestPluginFailure(t *testing.T) {
	pluginErr := errors.New("plugin initialization failed")
	client, err := New(
		Config{Driver: SQLiteConfig{Path: ":memory:"}},
		WithPlugins(failingPlugin{err: pluginErr}),
	)
	assert.Nil(t, client)
	assert.ErrorIs(t, err, pluginErr)
	assert.NotContains(t, err.Error(), ":memory:")
}
