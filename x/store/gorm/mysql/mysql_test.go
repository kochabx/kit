package mysql_test

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/kochabx/kit/x/store/gorm/mysql"
)

func TestBuildMySQLConfigTCPUnixAndTLSClone(t *testing.T) {
	t.Parallel()

	callerTLS := &tls.Config{ServerName: "mysql.internal", MinVersion: tls.VersionTLS13}

	tcpCfg, err := mysql.BuildMySQLConfig(mysql.Config{
		Host:     "mysql.internal",
		Database: "app",
		Username: "root",
		Password: "p@ss/word?&#=",
		TLS:      callerTLS,
	})
	if err != nil {
		t.Fatalf("BuildMySQLConfig TCP failed: %v", err)
	}
	if tcpCfg.Net != "tcp" || tcpCfg.Addr != "mysql.internal:3306" {
		t.Fatalf("unexpected TCP net/addr: %s / %s", tcpCfg.Net, tcpCfg.Addr)
	}
	if !tcpCfg.ParseTime {
		t.Fatal("expected ParseTime default true")
	}
	callerTLS.ServerName = "mutated.example"
	if tcpCfg.TLS.ServerName != "mysql.internal" {
		t.Fatalf("TLS not cloned, got %q", tcpCfg.TLS.ServerName)
	}

	unixCfg, err := mysql.BuildMySQLConfig(mysql.Config{
		UnixSocket: "/var/run/mysqld/mysqld.sock",
		Database:   "app",
		ParseTime:  new(false),
	})
	if err != nil {
		t.Fatalf("BuildMySQLConfig Unix failed: %v", err)
	}
	if unixCfg.Net != "unix" || unixCfg.Addr != "/var/run/mysqld/mysqld.sock" {
		t.Fatalf("unexpected unix net/addr: %s / %s", unixCfg.Net, unixCfg.Addr)
	}
	if unixCfg.ParseTime {
		t.Fatal("expected explicit ParseTime=false preserved")
	}
	if _, hasRawCharset := tcpCfg.Params["charset"]; hasRawCharset {
		t.Fatalf("charset must not be in Params (causes Error 1193): %+v", tcpCfg.Params)
	}

	dsnTLS, err := mysql.BuildMySQLConfig(mysql.Config{
		DSN: "user:pass@tcp(127.0.0.1:3306)/app?tls=preferred",
		TLS: callerTLS,
	})
	if err != nil {
		t.Fatalf("BuildMySQLConfig DSN+TLS failed: %v", err)
	}
	if dsnTLS.AllowFallbackToPlaintext || dsnTLS.TLS == nil {
		t.Fatalf("expected strict TLS without plaintext fallback, got %+v", dsnTLS)
	}
}

func TestConfigValidateAndLazyNew(t *testing.T) {
	t.Parallel()

	if _, err := mysql.New(mysql.Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}

	client, err := mysql.New(mysql.Config{
		Host:     "127.0.0.1",
		Port:     1,
		Database: "app",
		Timeout:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("lazy New failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("expected HealthCheck error for unreachable host")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
