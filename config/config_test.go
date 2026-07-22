package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/validator"
)

type api struct {
	Name     string            `json:"name"`
	Method   string            `json:"method" default:"GET"`
	Url      string            `json:"url"`
	Body     string            `json:"body"`
	Headers  map[string]string `json:"headers" default:"env: production"`
	Timeout  int               `json:"timeout" default:"3"`
	Period   int               `json:"period" default:"10"`
	Duration time.Duration     `json:"duration" default:"5s"`
}

type server struct {
	Host string `json:"host" default:"localhost"`
	Port int    `json:"port" default:"80"`
}

type mock struct {
	IDs     []string `json:"ids" default:"1,2,3"`
	Number  float64  `json:"number" default:"1.23"`
	Enabled bool     `json:"enabled" default:"true"`
	Server  server   `json:"server"`
	Apis    []api    `json:"apis"`
	Address struct {
		Street string `json:"street" default:"123 Main St"`
		City   string `json:"city" default:"Anytown"`
	} `json:"address"`
}

// TestConfig tests the basic configuration loading
func TestConfig(t *testing.T) {
	cfg := new(mock)

	// Use simplified API with defaults
	c := New(cfg)

	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	t.Logf("Config loaded: %+v", cfg)
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("server:\n  host: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed := make(chan struct{}, 1)
	cfg := new(mock)
	v := viper.New()
	loader := NewFileLoader("config.yaml", []string{dir}, v, validator.Validate)
	c := New(cfg, WithViper(v), WithLoader(loader), WithOnChange(func() {
		changed <- struct{}{}
	}))

	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	if err := c.Watch(); err != nil {
		t.Fatal(err)
	}
	if err := c.Watch(); err != nil {
		t.Fatalf("second Watch call should be idempotent: %v", err)
	}

	if err := os.WriteFile(configFile, []byte("server:\n  host: after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case <-changed:
		if cfg.Server.Host != "after" {
			t.Fatalf("expected reloaded host %q, got %q", "after", cfg.Server.Host)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for config reload")
	}
}

// TestEnvOverride tests that viper's AutomaticEnv works
func TestEnvOverride(t *testing.T) {
	// Set environment variables (viper automatically reads them)
	t.Setenv("SERVER_HOST", "example.com")
	t.Setenv("SERVER_PORT", "9090")

	cfg := new(mock)
	c := New(cfg)

	if err := c.Load(); err != nil {
		t.Fatal(err)
	}

	// Viper automatically applies environment variable overrides
	if cfg.Server.Host != "example.com" {
		t.Fatalf("expected SERVER_HOST override, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("expected SERVER_PORT override, got %d", cfg.Server.Port)
	}
}

func TestEnvExpansionInConfigFile(t *testing.T) {
	t.Setenv("SECRET", "test-secret")

	dir := t.TempDir()
	configFile := filepath.Join(dir, "example.yaml")
	content := []byte(`
targets:
  test:
    secret: "${SECRET}"
`)
	if err := os.WriteFile(configFile, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		Targets map[string]struct {
			Secret string `json:"secret"`
		} `json:"targets"`
	}

	v := viper.New()
	loader := NewFileLoader("example.yaml", []string{dir}, v, nil)
	if err := loader.Load(&cfg); err != nil {
		t.Fatal(err)
	}

	target := cfg.Targets["test"]
	if target.Secret != "test-secret" {
		t.Fatalf("expected expanded secret, got %q", target.Secret)
	}
}

func TestLoadIsTransactional(t *testing.T) {
	type validatedConfig struct {
		Host string `json:"host" default:"localhost"`
		Port int    `json:"port" validate:"min=1,max=65535"`
	}

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("host: before\nport: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := new(validatedConfig)
	v := viper.New()
	c := New(cfg,
		WithViper(v),
		WithLoader(NewFileLoader("config.yaml", []string{dir}, v, validator.Validate)),
	)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(configFile, []byte("host: after\nport: 70000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Load(); err == nil {
		t.Fatal("expected validation error")
	}
	if cfg.Host != "before" || cfg.Port != 8080 {
		t.Fatalf("expected target to remain unchanged, got %+v", cfg)
	}
}

func TestLoadClearsRemovedValues(t *testing.T) {
	type appConfig struct {
		Host string `json:"host" default:"localhost"`
		Port int    `json:"port"`
	}

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("host: custom\nport: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := new(appConfig)
	v := viper.New()
	c := New(cfg, WithViper(v), WithLoader(NewFileLoader("config.yaml", []string{dir}, v, nil)))
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(configFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "localhost" || cfg.Port != 0 {
		t.Fatalf("expected removed values to be reset, got %+v", cfg)
	}
}

func TestNewRejectsInvalidTargetOnLoad(t *testing.T) {
	c := New(nil)
	if err := c.Load(); err == nil {
		t.Fatal("expected invalid target error")
	}
}
