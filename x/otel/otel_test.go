package otel

import (
	"bytes"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kochabx/kit/log"
)

func TestLogExtractorWithRealSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(t.Context()) }()

	var buf bytes.Buffer
	logger := log.New(
		log.WithWriter(&buf),
		log.WithNoColor(true),
		log.WithContextExtractor(LogExtractor()),
	)

	// Without span context: no trace_id emitted.
	logger.InfoContext(t.Context(), "no-span")
	if strings.Contains(buf.String(), "trace_id=") {
		t.Fatalf("unexpected trace_id without active span: %s", buf.String())
	}

	// With active span context: trace_id and span_id emitted accurately.
	buf.Reset()
	ctx, span := tp.Tracer("test").Start(t.Context(), "op")
	defer span.End()
	wantTraceID := span.SpanContext().TraceID().String()
	wantSpanID := span.SpanContext().SpanID().String()

	logger.InfoContext(ctx, "with-span")
	out := buf.String()
	if !strings.Contains(out, "trace_id="+wantTraceID) || !strings.Contains(out, "span_id="+wantSpanID) {
		t.Fatalf("missing trace/span IDs in log %q (want trace=%s span=%s)", out, wantTraceID, wantSpanID)
	}
}

func TestConfigValidateAndLazyProviderLifecycle(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for empty config, got %v", err)
	}
	if _, err := New(Config{ServiceName: "svc", Endpoint: "http://127.0.0.1:4318", SampleRatio: new(1.5)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for invalid sample ratio, got %v", err)
	}

	// Trace/metric explicitly disabled with new(false) allows empty Endpoint.
	disabledProv, err := New(Config{
		ServiceName:  "svc",
		EnableTrace:  new(false),
		EnableMetric: new(false),
	})
	if err != nil {
		t.Fatalf("New with trace/metric disabled failed: %v", err)
	}
	if err := disabledProv.HealthCheck(t.Context()); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("HealthCheck before Start = %v, want ErrNotStarted", err)
	}

	// Reject insecure + TLS combination cleanly.
	if _, err := New(Config{
		ServiceName: "svc",
		Endpoint:    "http://127.0.0.1:4318",
		Insecure:    true,
		TLS:         &tls.Config{ServerName: "otel.internal"},
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for Insecure+TLS, got %v", err)
	}

	// Verify TLS & Headers cloning on HTTPS endpoint without hitting plaintext mismatch.
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer tlsSrv.Close()

	tlsCfg := &tls.Config{ServerName: "otel.internal", InsecureSkipVerify: true}
	headers := map[string]string{"authorization": "Bearer abc"}
	tlsProv, err := New(Config{
		ServiceName: "tls-svc",
		Endpoint:    tlsSrv.URL,
		Headers:     headers,
		TLS:         tlsCfg,
	}, WithRegisterGlobal(false))
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg.ServerName = "mutated"
	headers["authorization"] = "mutated"
	if tlsProv.cfg.TLS.ServerName != "otel.internal" || tlsProv.cfg.Headers["authorization"] != "Bearer abc" {
		t.Fatal("Provider mutated caller TLS or Headers")
	}
	if err := tlsProv.Start(t.Context()); err != nil {
		t.Fatalf("TLS Start failed: %v", err)
	}
	if err := tlsProv.Close(); err != nil {
		t.Fatalf("TLS Close failed: %v", err)
	}

	// Start against local plaintext httptest OTLP endpoint and verify full lifecycle.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	prov, err := New(Config{
		ServiceName:    "orders",
		ServiceVersion: "v2.0.0",
		Environment:    "test",
		Endpoint:       srv.URL,
		Insecure:       true,
		SampleRatio:    new(0.5),
	}, WithRegisterGlobal(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := prov.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := prov.Start(t.Context()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("duplicate Start = %v, want ErrAlreadyStarted", err)
	}
	if err := prov.HealthCheck(t.Context()); err != nil {
		t.Fatalf("HealthCheck after Start failed: %v", err)
	}
	if err := prov.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := prov.Close(); err != nil {
		t.Fatalf("idempotent Close failed: %v", err)
	}
	if err := prov.HealthCheck(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("HealthCheck after Close = %v, want ErrClosed", err)
	}

	// Hostname starting with 'h' without scheme must use WithEndpoint + WithInsecure cleanly.
	hostProv, err := New(Config{
		ServiceName: "host-h-svc",
		Endpoint:    "host.docker.internal:4318",
		Insecure:    true,
	}, WithRegisterGlobal(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := hostProv.Start(t.Context()); err != nil {
		t.Fatalf("Start with host.docker.internal:4318 failed: %v", err)
	}
	_ = hostProv.Close()
}

func TestRootURLSignalPaths(t *testing.T) {
	paths := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	prov, err := New(Config{
		ServiceName: "root-url-svc",
		Endpoint:    srv.URL,
		Insecure:    true,
	}, WithRegisterGlobal(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := prov.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	_, span := prov.TracerProvider().Tracer("test").Start(t.Context(), "op")
	span.End()
	c, mErr := prov.MeterProvider().Meter("test").Int64Counter("reqs")
	if mErr != nil {
		t.Fatal(mErr)
	}
	c.Add(t.Context(), 1)

	if err := prov.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	close(paths)

	var seenTraces, seenMetrics bool
	for p := range paths {
		switch p {
		case "/v1/traces":
			seenTraces = true
		case "/v1/metrics":
			seenMetrics = true
		}
	}
	if !seenTraces || !seenMetrics {
		t.Fatalf("expected /v1/traces and /v1/metrics requests, got traces=%v metrics=%v", seenTraces, seenMetrics)
	}
}
