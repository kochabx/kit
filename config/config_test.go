package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/kochabx/kit/x/validate"
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
	ServiceName string `json:"service_name"`
}

type contextKey struct{}

func (cfg *customConfig) Validate(ctx context.Context) error {
	if ctx.Value(contextKey{}) != "allowed" {
		return errors.New("missing validation context")
	}
	if cfg.ServiceName == "forbidden" {
		return errors.New("forbidden service_name")
	}
	return nil
}

type stubValidator struct {
	err error
}

func (v stubValidator) Struct(context.Context, any) error { return v.err }

func writeConfig(t testing.TB, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileViper(filename string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(filename)
	v.SetConfigType("yaml")
	return v
}

func assertNoLeak(t *testing.T, baseline int) {
	t.Helper()
	runtime.GC()
	if got := pprof.Lookup("goroutineleak").Count(); got > baseline {
		t.Fatalf("goroutine leak detected: count=%d baseline=%d", got, baseline)
	}
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	if _, err := New[testConfig](WithViper(nil)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("WithViper(nil) = %v, want ErrInvalidOptions", err)
	}
	if _, err := New[testConfig](WithFile("")); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("WithFile(\"\") = %v, want ErrInvalidOptions", err)
	}
	if _, err := New[int](); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New[int] = %v, want ErrInvalidOptions", err)
	}
	if _, err := New[testConfig](WithValidator(nil)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("WithValidator(nil) = %v, want ErrInvalidOptions", err)
	}
	if _, err := New[testConfig](WithTag("")); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("WithTag(\"\") = %v, want ErrInvalidOptions", err)
	}
	if _, err := New[testConfig](WithWatchDebounce(-time.Millisecond)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("WithWatchDebounce(-1ms) = %v, want ErrInvalidOptions", err)
	}
}

func TestDefaultFileEnvironmentAndExpansion(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, filepath.Join(directory, "config.yaml"), "server:\n  host: ${CONFIG_HOST}\n  port: 8080\n")
	t.Setenv("CONFIG_HOST", "expanded")
	t.Setenv("SERVER_PORT", "9090")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	cfg, err := New[testConfig]()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := cfg.Load(t.Context())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Server.Host != "expanded" || loaded.Server.Port != 9090 {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
}

func TestLoadWithDefaultsExplicitZeroAndPflagPrecedence(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  enabled: false\n  port: 8000\n")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("port", 0, "")
	if err := flags.Parse([]string{"--port=9100"}); err != nil {
		t.Fatal(err)
	}

	v := fileViper(filename)
	if err := v.BindPFlag("server.port", flags.Lookup("port")); err != nil {
		t.Fatal(err)
	}

	loader, err := New[testConfig](
		WithViper(v),
		WithTag("json"),
	)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := loader.Load(t.Context())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.Host != "localhost" || cfg.Server.Port != 9100 || cfg.Server.Enabled {
		t.Fatalf("unexpected decoded config: %+v", cfg)
	}
	if cfg.Labels["env"] != "development" || loader.Current() != cfg {
		t.Fatalf("labels or Current mismatch: %+v", cfg)
	}
}

func TestDefaultAndValidateTags(t *testing.T) {
	t.Parallel()

	// 1. Minimal YAML populates all default tags cleanly.
	minimalFile := filepath.Join(t.TempDir(), "minimal.yaml")
	writeConfig(t, minimalFile, "{}\n")
	minimalLoader, err := New[testConfig](WithFile(minimalFile))
	if err != nil {
		t.Fatal(err)
	}
	minimalCfg, err := minimalLoader.Load(t.Context())
	if err != nil {
		t.Fatalf("Load minimal failed: %v", err)
	}
	if minimalCfg.Server.Host != "localhost" || minimalCfg.Server.Port != 8080 || !minimalCfg.Server.Enabled {
		t.Fatalf("unexpected default tag values: %+v", minimalCfg)
	}
	if minimalCfg.Labels["env"] != "development" {
		t.Fatalf("unexpected default labels: %+v", minimalCfg.Labels)
	}

	// 2. Explicit empty string overrides default:"localhost" and fails validate:"required".
	emptyHostFile := filepath.Join(t.TempDir(), "empty_host.yaml")
	writeConfig(t, emptyHostFile, "server:\n  host: \"\"\n")
	emptyHostLoader, err := New[testConfig](WithFile(emptyHostFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emptyHostLoader.Load(t.Context()); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for empty host, got %v", err)
	} else if violations, ok := validate.Violations(err); !ok || len(violations) != 1 || violations[0].Field != "host" || violations[0].Tag != "required" {
		t.Fatalf("unexpected violations for empty host: ok=%v violations=%+v", ok, violations)
	}

	// 3. Out-of-range port fails validate:"gte=1,lte=65535".
	badPortFile := filepath.Join(t.TempDir(), "bad_port.yaml")
	writeConfig(t, badPortFile, "server:\n  port: 70000\n")
	badPortLoader, err := New[testConfig](WithFile(badPortFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badPortLoader.Load(t.Context()); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for out-of-range port, got %v", err)
	} else if violations, ok := validate.Violations(err); !ok || len(violations) != 1 || violations[0].Field != "port" || violations[0].Tag != "lte" {
		t.Fatalf("unexpected violations for out-of-range port: ok=%v violations=%+v", ok, violations)
	}

	// 4. Invalid default tag returns ErrDecode.
	type badDefaultConfig struct {
		Port int `json:"port" default:"not-an-int"`
	}
	badDefaultFile := filepath.Join(t.TempDir(), "bad_default.yaml")
	writeConfig(t, badDefaultFile, "{}\n")
	badDefaultLoader, err := New[badDefaultConfig](WithFile(badDefaultFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badDefaultLoader.Load(t.Context()); !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode for invalid default tag, got %v", err)
	}

	// 5. WithValidator overrides default tag validation while preserving default struct tags.
	overrideFile := filepath.Join(t.TempDir(), "override.yaml")
	writeConfig(t, overrideFile, "server:\n  port: 70000\n")
	overrideLoader, err := New[testConfig](
		WithFile(overrideFile),
		WithValidator(stubValidator{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	overrideCfg, err := overrideLoader.Load(t.Context())
	if err != nil {
		t.Fatalf("Load with validator override failed: %v", err)
	}
	if overrideCfg.Server.Host != "localhost" || overrideCfg.Server.Port != 70000 {
		t.Fatalf("unexpected default tag or validator override result: %+v", overrideCfg)
	}
}

func TestLoadPreservesPreviousSnapshotOnFailure(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: before\n  port: 8080\n")
	loader, err := New[testConfig](WithFile(filename))
	if err != nil {
		t.Fatal(err)
	}
	before, err := loader.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filename, "server:\n  host: after\n  port: 70000\n")
	if after, err := loader.Load(t.Context()); after != nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got (%v, %v)", after, err)
	}
	if loader.Current() != before || loader.Current().Server.Host != "before" {
		t.Fatal("previous snapshot overwritten on port validation failure")
	}
}

func TestCustomValidationAndValidatorHook(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "custom.yaml")
	writeConfig(t, filename, "service_name: allowed\n")

	loader, err := New[customConfig](WithFile(filename))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(t.Context()); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation without ctx key, got %v", err)
	}
	ctx := context.WithValue(t.Context(), contextKey{}, "allowed")
	cfg, err := loader.Load(ctx)
	if err != nil || cfg.ServiceName != "allowed" {
		t.Fatalf("Load with ctx failed: (%+v, %v)", cfg, err)
	}

	okFile := filepath.Join(t.TempDir(), "ok.yaml")
	writeConfig(t, okFile, "server:\n  host: ok\n  port: 8080\n")
	failingLoader, err := New[testConfig](
		WithFile(okFile),
		WithValidator(stubValidator{err: errors.New("tag invalid")}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failingLoader.Load(t.Context()); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation from Validator, got %v", err)
	}
}

func TestWatchPreconditions(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: ok\n  port: 8080\n")

	loader, err := New[testConfig](WithFile(filename))
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Watch(t.Context(), func(Event[testConfig]) {}); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("expected ErrNotLoaded, got %v", err)
	}
	if _, err := loader.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := loader.Watch(t.Context(), nil); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected ErrInvalidOptions for nil handler, got %v", err)
	}
}

func TestSafeEnvExpansionLiteralDollarsInjectionAndWatchCloseRace(t *testing.T) {
	// 1. Literal $tokens (bcrypt/SQL) preserved; multi-line env var cannot inject YAML keys; JSON ${VAR} works.
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "safe.yaml")
	writeConfig(t, yamlPath, "server:\n  host: ${SAFE_HOST}\n  port: 8080\nlabels:\n  bcrypt: '$2a$10$abcdef'\n  sql: 'SELECT $1'\n")
	t.Setenv("SAFE_HOST", "ok\n  port: 1")

	loader, err := New[testConfig](WithFile(yamlPath))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loader.Load(t.Context())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.Host != "ok\n  port: 1" || cfg.Server.Port != 8080 {
		t.Fatalf("YAML injection occurred or value mangled: %+v", cfg)
	}
	if cfg.Labels["bcrypt"] != "$2a$10$abcdef" || cfg.Labels["sql"] != "SELECT $1" {
		t.Fatalf("literal $ values corrupted: %+v", cfg.Labels)
	}

	jsonPath := filepath.Join(dir, "config.json")
	writeConfig(t, jsonPath, `{"server":{"host":"${JSON_HOST}","port":9090}}`)
	t.Setenv("JSON_HOST", "json.internal")
	jsonLoader, err := New[testConfig](WithFile(jsonPath))
	if err != nil {
		t.Fatal(err)
	}
	jsonCfg, err := jsonLoader.Load(t.Context())
	if err != nil || jsonCfg.Server.Host != "json.internal" {
		t.Fatalf("JSON env expansion = (%+v, %v)", jsonCfg, err)
	}

	// 2. Concurrent Watch + Close race stress + WatchSeq early break (watchDelay=0).
	for range 10 {
		wFile := filepath.Join(t.TempDir(), "race.yaml")
		writeConfig(t, wFile, "server:\n  host: init\n  port: 8080\n")
		wl, err := New[testConfig](WithFile(wFile), WithWatchDebounce(0))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wl.Load(t.Context()); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = wl.Watch(t.Context(), func(Event[testConfig]) {})
		}()
		_ = wl.Close()
		<-done
	}
}

func TestFileWatchAndWatchSeqNoLeak(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() { assertNoLeak(t, baseline) })

	filename := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, filename, "server:\n  host: before\n  port: 8080\n")

	loader, err := New[testConfig](WithFile(filename), WithWatchDebounce(15*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	before, err := loader.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan Event[testConfig], 4)
	if err := loader.Watch(t.Context(), func(ev Event[testConfig]) { events <- ev }); err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	if err := loader.Watch(t.Context(), func(Event[testConfig]) {}); !errors.Is(err, ErrAlreadyWatching) {
		t.Fatalf("expected ErrAlreadyWatching, got %v", err)
	}
	if _, err := loader.Load(t.Context()); !errors.Is(err, ErrAlreadyWatching) {
		t.Fatalf("Load during Watch = %v, want ErrAlreadyWatching", err)
	}

	// Invalid reload preserves previous snapshot and reports ErrValidation.
	writeConfig(t, filename, "server:\n  host: bad\n  port: 70000\n")
	invalid := awaitEvent(t, events)
	if !errors.Is(invalid.Err, ErrValidation) || invalid.Previous != before || loader.Current() != before {
		t.Fatalf("unexpected invalid event or snapshot change: %+v", invalid)
	}

	// Valid reload publishes new snapshot cleanly.
	writeConfig(t, filename, "server:\n  host: after\n  port: 9090\n")
	valid := awaitEvent(t, events)
	if valid.Err != nil || valid.Current == nil || valid.Current.Server.Host != "after" || valid.Current.Server.Port != 9090 {
		t.Fatalf("unexpected valid event: %+v", valid)
	}
	if loader.Current() != valid.Current || before.Server.Host != "before" {
		t.Fatal("snapshot immutability or update failed")
	}

	if err := loader.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Also verify WatchSeq iterator exits cleanly with zero leaks.
	seqFile := filepath.Join(t.TempDir(), "seq.yaml")
	writeConfig(t, seqFile, "server:\n  host: s1\n  port: 8080\n")
	seqLoader, err := New[testConfig](WithFile(seqFile), WithWatchDebounce(15*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = seqLoader.Close() }()
	if _, err := seqLoader.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{})
	seqLoader.watchReady = func() { close(ready) }

	seqCtx, seqCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer seqCancel()
	done := make(chan struct{})
	var seqGot *testConfig
	go func() {
		defer close(done)
		for ev := range seqLoader.WatchSeq(seqCtx) {
			if ev.Current != nil && ev.Current.Server.Host == "s2" {
				seqGot = ev.Current
				return
			}
		}
	}()

	<-ready
	writeConfig(t, seqFile, "server:\n  host: s2\n  port: 8081\n")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for WatchSeq")
	}
	if seqGot == nil || seqGot.Server.Port != 8081 {
		t.Fatalf("unexpected WatchSeq result: %+v", seqGot)
	}
}

func TestConcurrentWatchAndCloseRace(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() { assertNoLeak(t, baseline) })

	for range 20 {
		wFile := filepath.Join(t.TempDir(), "race.yaml")
		writeConfig(t, wFile, "server:\n  host: init\n  port: 8080\n")
		wl, err := New[testConfig](WithFile(wFile), WithWatchDebounce(5*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wl.Load(t.Context()); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			_ = wl.Watch(t.Context(), func(Event[testConfig]) {})
		})
		for range 4 {
			wg.Go(func() {
				<-start
				_ = wl.Close()
			})
		}
		close(start)
		wg.Wait()
	}

	for range 20 {
		seqFile := filepath.Join(t.TempDir(), "seq-race.yaml")
		writeConfig(t, seqFile, "server:\n  host: init\n  port: 8080\n")
		sl, err := New[testConfig](WithFile(seqFile), WithWatchDebounce(5*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sl.Load(t.Context()); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			for range sl.WatchSeq(t.Context()) {
				return
			}
		})
		for range 4 {
			wg.Go(func() {
				<-start
				_ = sl.Close()
			})
		}
		close(start)
		wg.Wait()
	}
}

func TestWatchAndWatchSeqCloseInsideCallbackNoDeadlock(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() { assertNoLeak(t, baseline) })

	// 1. Calling Close() directly inside Watch handler must return without self-deadlock.
	wFile := filepath.Join(t.TempDir(), "watch-self-close.yaml")
	writeConfig(t, wFile, "server:\n  host: init\n  port: 8080\n")
	wl, err := New[testConfig](WithFile(wFile), WithWatchDebounce(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wl.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	callbackDone := make(chan error, 1)
	if err := wl.Watch(t.Context(), func(Event[testConfig]) {
		callbackDone <- wl.Close()
	}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, wFile, "server:\n  host: updated\n  port: 8081\n")
	select {
	case closeErr := <-callbackDone:
		if closeErr != nil {
			t.Fatalf("Close inside Watch callback failed: %v", closeErr)
		}
	case <-time.After(3 * time.Second):
		_ = wl.Close()
		t.Fatal("Close inside Watch callback deadlocked")
	}

	// 2. Calling Close() directly inside WatchSeq loop body must return without self-deadlock.
	sFile := filepath.Join(t.TempDir(), "seq-self-close.yaml")
	writeConfig(t, sFile, "server:\n  host: init\n  port: 8080\n")
	sl, err := New[testConfig](WithFile(sFile), WithWatchDebounce(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sl.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	sl.watchReady = func() { close(ready) }

	seqClosed := make(chan error, 1)
	seqExited := make(chan struct{})
	go func() {
		defer close(seqExited)
		for range sl.WatchSeq(t.Context()) {
			seqClosed <- sl.Close()
		}
	}()

	<-ready
	writeConfig(t, sFile, "server:\n  host: updated\n  port: 8082\n")
	select {
	case closeErr := <-seqClosed:
		if closeErr != nil {
			t.Fatalf("Close inside WatchSeq body failed: %v", closeErr)
		}
	case <-time.After(3 * time.Second):
		_ = sl.Close()
		t.Fatal("Close inside WatchSeq body deadlocked")
	}
	<-seqExited
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

func TestWatchEmitsErrorOnDeleteAndRecoversOnRecreate(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() { assertNoLeak(t, baseline) })

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "app.yaml")
	writeConfig(t, cfgFile, "server:\n  host: initial\n  port: 8080\n")

	loader, err := New[testConfig](WithFile(cfgFile), WithWatchDebounce(15*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Close() }()

	if _, err := loader.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	events := make(chan Event[testConfig], 4)
	if err := loader.Watch(t.Context(), func(ev Event[testConfig]) {
		events <- ev
	}); err != nil {
		t.Fatal(err)
	}

	// Removing watched file emits an error event while keeping last valid snapshot.
	if err := os.Remove(cfgFile); err != nil {
		t.Fatal(err)
	}
	delEv := awaitEvent(t, events)
	if delEv.Err == nil {
		t.Fatalf("expected error event when config file removed, got %+v", delEv)
	}
	if snap := loader.Current(); snap == nil || snap.Server.Host != "initial" {
		t.Fatalf("snapshot not preserved across delete error: %+v", snap)
	}

	// Atomically replacing the config file recovers watching and updates snapshot.
	tmp := filepath.Join(dir, "app.yaml.new")
	writeConfig(t, tmp, "server:\n  host: recovered\n  port: 9090\n")
	if err := os.Rename(tmp, cfgFile); err != nil {
		t.Fatal(err)
	}
	recEv := awaitEvent(t, events)
	if recEv.Err != nil || recEv.Current == nil || recEv.Current.Server.Host != "recovered" {
		t.Fatalf("expected recovery reload event, got %+v", recEv)
	}
}
