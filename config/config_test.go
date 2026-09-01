package config

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Server struct {
		Host    string `json:"host" default:"localhost" validate:"required"`
		Port    int    `json:"port" default:"8080" validate:"gte=1,lte=65535"`
		Enabled bool   `json:"enabled" default:"true"`
	} `json:"server"`
	Labels map[string]string `json:"labels" default:"{\"env\":\"development\"}"`
}

type customConfig struct {
	Name string `json:"name"`
}

type contextKey struct{}

type passValidator struct{}

func (passValidator) Struct(context.Context, any) error      { return nil }
func (passValidator) Var(context.Context, any, string) error { return nil }

type fakeRemote struct {
	mu      sync.RWMutex
	content string
}

func (remote *fakeRemote) Get(viper.RemoteProvider) (io.Reader, error) {
	remote.mu.RLock()
	defer remote.mu.RUnlock()
	return strings.NewReader(remote.content), nil
}

func (remote *fakeRemote) Watch(provider viper.RemoteProvider) (io.Reader, error) {
	return remote.Get(provider)
}

func (*fakeRemote) WatchChannel(viper.RemoteProvider) (<-chan *viper.RemoteResponse, chan bool) {
	return nil, nil
}

func (remote *fakeRemote) set(content string) {
	remote.mu.Lock()
	remote.content = content
	remote.mu.Unlock()
}

func (cfg *customConfig) Validate(ctx context.Context) error {
	if ctx.Value(contextKey{}) != "allowed" {
		return errors.New("missing validation context")
	}
	if cfg.Name == "forbidden" {
		return errors.New("forbidden name")
	}
	return nil
}

func fileViper(filename string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(filename)
	v.SetConfigType("yaml")
	return v
}

func writeConfig(t testing.TB, filename, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
}

func TestNewValidation(t *testing.T) {
	_, err := New[testConfig](WithViper(nil))
	assert.ErrorIs(t, err, ErrInvalidOptions)
	_, err = New[int](WithSource(SourceValues))
	assert.ErrorIs(t, err, ErrInvalidOptions)
	_, err = New[testConfig](WithSource("invalid"))
	assert.ErrorIs(t, err, ErrUnsupportedSource)
	for _, remote := range []struct {
		provider   string
		endpoint   string
		path       string
		configType string
		interval   time.Duration
	}{
		{endpoint: "endpoint", path: "path", configType: "yaml", interval: time.Second},
		{provider: "etcd3", path: "path", configType: "yaml", interval: time.Second},
		{provider: "etcd3", endpoint: "endpoint", configType: "yaml", interval: time.Second},
		{provider: "etcd3", endpoint: "endpoint", path: "path", interval: time.Second},
		{provider: "etcd3", endpoint: "endpoint", path: "path", configType: "yaml"},
	} {
		_, err = New[testConfig](WithRemote(
			remote.provider,
			remote.endpoint,
			remote.path,
			remote.configType,
			remote.interval,
		))
		assert.Error(t, err)
	}
	_, err = New[testConfig](WithSource(SourceRemote))
	assert.ErrorIs(t, err, ErrInvalidOptions)
	_, err = New[testConfig](WithSource(SourceRemote), WithViper(viper.New()))
	assert.ErrorIs(t, err, ErrInvalidOptions)
	_, err = New[testConfig](
		WithRemote("etcd3", "http://127.0.0.1:2379", "/app/config", "yaml", time.Second),
		WithSource(SourceFile),
	)
	assert.ErrorIs(t, err, ErrInvalidOptions)
	_, err = New[testConfig](WithValidator(nil))
	assert.ErrorIs(t, err, ErrInvalidOptions)
}

func TestDefaultFileEnvironmentAndExpansion(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, filepath.Join(directory, "config.yaml"), "server:\n  host: ${CONFIG_HOST}\n  port: 8080\n")
	t.Setenv("CONFIG_HOST", "expanded")
	t.Setenv("SERVER_PORT", "9090")

	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(directory))
	t.Cleanup(func() { _ = os.Chdir(previous) })

	cfg, err := New[testConfig]()
	require.NoError(t, err)
	loaded, err := cfg.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "expanded", loaded.Server.Host)
	assert.Equal(t, 9090, loaded.Server.Port)
}

func TestLoadFileDefaultsAndExplicitZero(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: api.internal\n  enabled: false\n")
	loader, err := New[testConfig](WithViper(fileViper(filename)))
	require.NoError(t, err)

	cfg, err := loader.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "api.internal", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.False(t, cfg.Server.Enabled, "an explicit zero value must override its default")
	assert.Equal(t, map[string]string{"env": "development"}, cfg.Labels)
	current := loader.current.Load()
	assert.Same(t, cfg, current)
}

func TestLoadPreservesPreviousSnapshotOnFailure(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: before\n  port: 8080\n")
	loader, err := New[testConfig](WithViper(fileViper(filename)))
	require.NoError(t, err)
	before, err := loader.Load(t.Context())
	require.NoError(t, err)

	writeConfig(t, filename, "server:\n  host: after\n  port: 70000\n")
	after, err := loader.Load(t.Context())
	assert.Nil(t, after)
	assert.ErrorIs(t, err, ErrValidation)
	current := loader.current.Load()
	assert.Same(t, before, current)
	assert.Equal(t, "before", current.Server.Host)
}

func TestViperPrecedenceFlagsEnvironmentAliasAndSet(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: file\n  port: 8000\n")
	t.Setenv("APP_SERVER__HOST", "environment")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("port", 0, "")
	require.NoError(t, flags.Parse([]string{"--port=9000"}))

	v := fileViper(filename)
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()
	require.NoError(t, v.BindEnv("server.host"))
	require.NoError(t, v.BindPFlag("server.port", flags.Lookup("port")))
	v.RegisterAlias("http.port", "server.port")
	v.Set("http.port", 9100)

	loader, err := New[testConfig](WithViper(v))
	require.NoError(t, err)
	cfg, err := loader.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "environment", cfg.Server.Host)
	assert.Equal(t, 9100, cfg.Server.Port)
}

func TestCallerOwnedViperSet(t *testing.T) {
	v := viper.New()
	v.Set("server.host", "before")
	loader, err := New[testConfig](WithSource(SourceValues), WithViper(v))
	require.NoError(t, err)
	before, err := loader.Load(t.Context())
	require.NoError(t, err)

	v.Set("server.host", "after")
	after, err := loader.Load(t.Context())
	require.NoError(t, err)
	assert.NotSame(t, before, after)
	assert.Equal(t, "before", before.Server.Host)
	assert.Equal(t, "after", after.Server.Host)
	current := loader.current.Load()
	assert.Same(t, after, current)

	v.Set("server.port", 70000)
	failed, err := loader.Load(t.Context())
	assert.Nil(t, failed)
	assert.ErrorIs(t, err, ErrValidation)
	current = loader.current.Load()
	assert.Same(t, after, current)
}

func TestCustomValidationReceivesContext(t *testing.T) {
	v := viper.New()
	v.Set("name", "allowed")
	loader, err := New[customConfig](WithSource(SourceValues), WithViper(v))
	require.NoError(t, err)

	_, err = loader.Load(t.Context())
	assert.ErrorIs(t, err, ErrValidation)
	ctx := context.WithValue(t.Context(), contextKey{}, "allowed")
	cfg, err := loader.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, "allowed", cfg.Name)
}

func TestCustomValidator(t *testing.T) {
	loader, err := New[testConfig](WithSource(SourceValues), WithValidator(passValidator{}), WithViper(viper.New()))
	require.NoError(t, err)
	_, err = loader.Load(t.Context())
	require.NoError(t, err)
}

func TestWatchRequiresLoadedWatchableSource(t *testing.T) {
	loader, err := New[testConfig](WithSource(SourceValues), WithViper(viper.New()))
	require.NoError(t, err)
	assert.ErrorIs(t, loader.Watch(func(Event[testConfig]) {}), ErrNotLoaded)
	_, err = loader.Load(t.Context())
	require.NoError(t, err)
	assert.ErrorIs(t, loader.Watch(nil), ErrInvalidOptions)
	assert.ErrorIs(t, loader.Watch(func(Event[testConfig]) {}), ErrUnsupportedSource)
}

func TestFileWatchPublishesOnlyValidChanges(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: before\n  port: 8080\n")
	loader, err := New[testConfig](WithViper(fileViper(filename)))
	require.NoError(t, err)
	before, err := loader.Load(t.Context())
	require.NoError(t, err)

	events := make(chan Event[testConfig], 4)
	require.NoError(t, loader.Watch(func(event Event[testConfig]) { events <- event }))
	assert.ErrorIs(t, loader.Watch(func(Event[testConfig]) {}), ErrAlreadyWatching)
	_, err = loader.Load(t.Context())
	assert.ErrorIs(t, err, ErrAlreadyWatching)

	writeConfig(t, filename, "server:\n  host: invalid\n  port: 70000\n")
	invalid := awaitEvent(t, events)
	assert.ErrorIs(t, invalid.Err, ErrValidation)
	assert.Same(t, before, invalid.Previous)
	current := loader.current.Load()
	assert.Same(t, before, current)

	writeConfig(t, filename, "server:\n  host: after\n  port: 9090\n")
	valid := awaitEvent(t, events)
	require.NoError(t, valid.Err)
	require.NotNil(t, valid.Current)
	assert.Equal(t, "after", valid.Current.Server.Host)
	assert.Equal(t, 9090, valid.Current.Server.Port)
	assert.Equal(t, "before", before.Server.Host)
}

func TestSnapshotIsSafeDuringFileWatch(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: before\n")
	loader, err := New[testConfig](WithViper(fileViper(filename)))
	require.NoError(t, err)
	_, err = loader.Load(t.Context())
	require.NoError(t, err)
	done := make(chan struct{})
	require.NoError(t, loader.Watch(func(event Event[testConfig]) {
		if event.Current != nil && event.Current.Server.Host == "after" {
			close(done)
		}
	}))

	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					current := loader.current.Load()
					_ = current.Server.Host
				}
			}
		})
	}
	writeConfig(t, filename, "server:\n  host: after\n")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watched update")
	}
	readers.Wait()
}

func TestDebouncerCoalescesCalls(t *testing.T) {
	var debounce debouncer
	var calls atomic.Int32
	fired := make(chan struct{}, 1)
	callback := func() {
		calls.Add(1)
		fired <- struct{}{}
	}

	for range 3 {
		debounce.schedule(20*time.Millisecond, callback)
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debounced callback")
	}
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRemoteWatchPublishesChanges(t *testing.T) {
	previousRemote := viper.RemoteConfig
	remote := &fakeRemote{content: "server:\n  host: before\n"}
	viper.RemoteConfig = remote
	t.Cleanup(func() { viper.RemoteConfig = previousRemote })

	loader, err := New[testConfig](
		WithRemote("etcd3", "http://127.0.0.1:2379", "/app/config", "yaml", 10*time.Millisecond),
	)
	require.NoError(t, err)
	before, err := loader.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "before", before.Server.Host)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan Event[testConfig], 1)
	require.NoError(t, loader.WatchRemote(ctx, func(event Event[testConfig]) { events <- event }))
	remote.set("server:\n  host: after\n")
	update := awaitEvent(t, events)
	require.NoError(t, update.Err)
	require.NotNil(t, update.Current)
	assert.Equal(t, "after", update.Current.Server.Host)
}

func awaitEvent[T any](t *testing.T, events <-chan Event[T]) Event[T] {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for config event")
		return Event[T]{}
	}
}
