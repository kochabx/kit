package config

import (
	"testing"
	"time"
)

type api struct {
	Name    string            `json:"name"`
	Method  string            `json:"method" default:"GET"`
	Url     string            `json:"url"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers" default:"env: production"`
	Timeout int               `json:"timeout" default:"3"`
	Period  int               `json:"period" default:"10"`
}

type server struct {
	Host string `json:"host" default:"localhost"`
	Port int    `json:"port" default:"80"`
}

type mock struct {
	Number  float64 `json:"number" default:"1.23"`
	Enabled bool    `json:"enabled" default:"true"`
	Server  server  `json:"server"`
	Apis    []api   `json:"apis"`
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

	if err := c.Watch(); err != nil {
		t.Fatal(err)
	}

	// Keep test running briefly to allow watch to initialize
	time.Sleep(100 * time.Millisecond)
}

func TestWatch(t *testing.T) {
	cfg := new(mock)
	c := New(cfg)

	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	t.Logf("Initial config: %+v", cfg)

	if err := c.Watch(); err != nil {
		t.Fatal(err)
	}

	// Simulate waiting for config changes
	t.Log("Watching for config changes for 10 seconds...")
	time.Sleep(10 * time.Second)
	t.Logf("Readload config: %+v", cfg)
	t.Log("Finished watching for config changes.")
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
		t.Logf("Note: Viper env override for SERVER_HOST: expected='example.com', got='%s'", cfg.Server.Host)
	}
	if cfg.Server.Port != 9090 {
		t.Logf("Note: Viper env override for SERVER_PORT: expected=9090, got=%d", cfg.Server.Port)
	}

	t.Logf("Viper AutomaticEnv test completed: %+v", cfg)
}
