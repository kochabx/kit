package asynq_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	kitasynq "github.com/kochabx/kit/x/asynq"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	if err := (kitasynq.Config{}).Validate(); err == nil {
		t.Fatal("expected error when neither RedisClient nor RedisOpt is set")
	}
	if err := (kitasynq.Config{
		RedisOpt:    asynq.RedisClientOpt{Addr: "127.0.0.1:6379"},
		Concurrency: -1,
	}).Validate(); err == nil {
		t.Fatal("expected error for negative concurrency")
	}
	if err := (kitasynq.Config{
		RedisOpt: asynq.RedisClientOpt{Addr: "127.0.0.1:6379"},
		Queues:   map[string]int{"critical": 0},
	}).Validate(); err == nil {
		t.Fatal("expected error for non-positive queue priority")
	}
}

func TestSlogLoggerAndOTelMiddleware(t *testing.T) {
	t.Parallel()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	mw := kitasynq.OTelMiddleware(kitasynq.WithTracerProvider(tp))
	task := asynq.NewTask("email:send", []byte(`{"id":1}`))

	okHandler := mw(asynq.HandlerFunc(func(_ context.Context, got *asynq.Task) error {
		if got.Type() != "email:send" {
			t.Fatalf("unexpected task type %q", got.Type())
		}
		return nil
	}))
	if err := okHandler.ProcessTask(t.Context(), task); err != nil {
		t.Fatalf("okHandler failed: %v", err)
	}

	wantErr := errors.New("smtp timeout")
	errHandler := mw(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return wantErr
	}))
	if err := errHandler.ProcessTask(t.Context(), task); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}

	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("recorded %d spans, want 2", len(spans))
	}
	if spans[0].Name != "asynq.process email:send" {
		t.Fatalf("span[0].Name = %q", spans[0].Name)
	}
	if spans[1].Status.Code != codes.Error {
		t.Fatalf("span[1].Status.Code = %v, want Error", spans[1].Status.Code)
	}

	// Lazy global TracerProvider resolution works even when OTelMiddleware is constructed first.
	prevTP := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevTP) })
	otel.SetTracerProvider(noop.NewTracerProvider()) // noop initially
	lazyMW := kitasynq.OTelMiddleware()
	lateExp := tracetest.NewInMemoryExporter()
	lateTP := sdktrace.NewTracerProvider(sdktrace.WithSyncer(lateExp))
	defer func() { _ = lateTP.Shutdown(t.Context()) }()
	otel.SetTracerProvider(lateTP)

	if err := lazyMW(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })).ProcessTask(t.Context(), task); err != nil {
		t.Fatalf("lazy OTelMiddleware ProcessTask failed: %v", err)
	}
	if len(lateExp.GetSpans()) != 1 {
		t.Fatalf("expected late-bound OTelMiddleware span, got %d", len(lateExp.GetSpans()))
	}
}

func TestClientServerSchedulerLifecycleConstruction(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// 1. Owned RedisOpt client closes cleanly.
	ownedCli, err := kitasynq.NewClient(kitasynq.Config{
		RedisOpt: asynq.RedisClientOpt{Addr: "127.0.0.1:6379"},
	})
	if err != nil {
		t.Fatalf("NewClient owned failed: %v", err)
	}
	if ownedCli.Client() == nil {
		t.Fatal("expected non-nil native *asynq.Client")
	}
	if err := ownedCli.Close(); err != nil {
		t.Fatalf("ownedCli.Close failed: %v", err)
	}
	if err := ownedCli.Stop(t.Context()); err != nil {
		t.Fatalf("idempotent Stop failed: %v", err)
	}

	// 2. Shared RedisClient client treats Close as a clean no-op instead of returning shared error.
	rdb := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	t.Cleanup(func() { _ = rdb.Close() })

	sharedCli, err := kitasynq.NewClient(kitasynq.Config{RedisClient: rdb})
	if err != nil {
		t.Fatalf("NewClient shared failed: %v", err)
	}
	if err := sharedCli.Close(); err != nil {
		t.Fatalf("sharedCli.Close must succeed as no-op, got: %v", err)
	}

	// 3. Server and Scheduler creation + Stop when not started.
	if _, err := kitasynq.NewServer(kitasynq.Config{RedisClient: rdb}, nil); err == nil {
		t.Fatal("expected error for nil handler")
	}
	srv, err := kitasynq.NewServer(
		kitasynq.Config{
			RedisClient: rdb,
			Concurrency: 4,
			Queues:      map[string]int{"critical": 6, "default": 3},
		},
		asynq.NewServeMux(),
		kitasynq.WithLogger(logger),
	)
	if err != nil || srv.Server() == nil {
		t.Fatalf("NewServer failed: srv=%v err=%v", srv, err)
	}
	if err := srv.Stop(t.Context()); err != nil {
		t.Fatalf("srv.Stop before start failed: %v", err)
	}

	sched, err := kitasynq.NewScheduler(
		kitasynq.Config{RedisClient: rdb},
		kitasynq.WithLogger(logger),
	)
	if err != nil || sched.Scheduler() == nil {
		t.Fatalf("NewScheduler failed: sched=%v err=%v", sched, err)
	}
	if err := sched.Stop(t.Context()); err != nil {
		t.Fatalf("sched.Stop before start failed: %v", err)
	}

	_ = strings.TrimSpace(logBuf.String())
}

func TestHealthCheckCancelsBlockedPing(t *testing.T) {
	t.Parallel()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	accepted := make(chan struct{}, 8)
	doneAccept := make(chan struct{})
	defer close(doneAccept)
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			select {
			case accepted <- struct{}{}:
			default:
			}
			go func(c net.Conn) {
				defer c.Close()
				<-doneAccept
			}(conn)
		}
	}()

	opt := asynq.RedisClientOpt{
		Addr:        lis.Addr().String(),
		DialTimeout: time.Second,
		ReadTimeout: 5 * time.Second,
	}
	cli, err := kitasynq.NewClient(kitasynq.Config{RedisOpt: opt})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Close() }()

	srv, err := kitasynq.NewServer(kitasynq.Config{RedisOpt: opt}, asynq.NewServeMux())
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.HealthCheck(t.Context()); err == nil {
		t.Fatal("expected HealthCheck to fail before Server.Start")
	}
	if err := srv.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop(t.Context()) }()

	sched, err := kitasynq.NewScheduler(kitasynq.Config{RedisOpt: opt})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.HealthCheck(t.Context()); err == nil {
		t.Fatal("expected HealthCheck to fail before Scheduler.Start")
	}
	if err := sched.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sched.Stop(t.Context()) }()

	for _, tc := range []struct {
		name string
		hc   func(context.Context) error
	}{
		{"Client", cli.HealthCheck},
		{"Server", srv.HealthCheck},
		{"Scheduler", sched.HealthCheck},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			errCh := make(chan error, 1)
			go func() {
				errCh <- tc.hc(ctx)
			}()
			select {
			case <-accepted:
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatalf("%s Ping never dialed silent listener", tc.name)
			}
			cancel()
			select {
			case err := <-errCh:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("%s HealthCheck = %v, want context.Canceled", tc.name, err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("%s HealthCheck ignored ctx cancellation", tc.name)
			}
		})
	}
}
