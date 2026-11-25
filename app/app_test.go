package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kochabx/kit/cx"
)

type testComponent struct {
	start func(context.Context) error
	stop  func(context.Context) error
}

func (c *testComponent) Start(ctx context.Context) error {
	if c.start != nil {
		return c.start(ctx)
	}
	return nil
}

func (c *testComponent) Stop(ctx context.Context) error {
	if c.stop != nil {
		return c.stop(ctx)
	}
	return nil
}

func (c *testComponent) HealthCheck(context.Context) error { return nil }

func assertNoGoroutineLeaks(t *testing.T, baseline int) {
	t.Helper()
	prof := pprof.Lookup("goroutineleak")
	if prof == nil {
		t.Fatal("goroutineleak profile unavailable")
	}
	runtime.GC()
	if got := prof.Count(); got > baseline {
		var buf bytes.Buffer
		_ = prof.WriteTo(&buf, 2)
		t.Fatalf("goroutine leak detected: count=%d (baseline=%d)\n%s", got, baseline, buf.String())
	}
}

func TestNewValidation(t *testing.T) {
	var nilServer *testComponent
	var nilRunnable cx.Runnable
	for _, tc := range []struct {
		name    string
		options []Option
	}{
		{"nil option", []Option{nil}},
		{"timeout", []Option{WithShutdownTimeout(0)}},
		{"nil logger", []Option{WithLogger(nil)}},
		{"nil container", []Option{WithContainer(nil)}},
		{"nil server", []Option{WithServers(nil)}},
		{"typed nil server", []Option{WithServers(nilServer)}},
		{"nil runnable", []Option{WithRunnable(nilRunnable)}},
		{"nil signal", []Option{WithSignals(nil)}},
		{"nil component", []Option{WithComponent(cx.NewKey[*testComponent]("component"), nilServer)}},
		{"empty key", []Option{WithComponent(cx.NewKey[int](""), 1)}},
		{"nil hook", []Option{WithOnStart(nil)}},
		{"external hook", []Option{WithContainer(cx.New()), WithOnStart(func(context.Context) error { return nil })}},
		{"external server", []Option{WithServers(&testComponent{}), WithContainer(cx.New())}},
		{"external runnable", []Option{WithContainer(cx.New()), WithRunnable(cx.RunnerFunc(func(context.Context) error { return nil }))}},
		{"external component", []Option{WithContainer(cx.New()), WithComponent(cx.NewKey[int]("value"), 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			application, err := New(tc.options...)
			if application != nil || !errors.Is(err, ErrInvalidOption) {
				t.Fatalf("New() = (%v, %v), want (nil, ErrInvalidOption)", application, err)
			}
		})
	}
}

func TestRunOrderHooksContextAndShutdown(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() { assertNoGoroutineLeaks(t, baseline) })

	type ctxKey struct{}
	var order []string
	record := func(name string) func(context.Context) error {
		return func(ctx context.Context) error {
			if name == "server-stop" || name == "dependency-stop" {
				if ctx.Err() != nil {
					return fmt.Errorf("expected active shutdown ctx: %w", ctx.Err())
				}
				if ctx.Value(ctxKey{}) != "value" {
					return errors.New("lost parent context value in shutdown")
				}
				if _, ok := ctx.Deadline(); !ok {
					return errors.New("missing shutdown deadline")
				}
			}
			order = append(order, name)
			return nil
		}
	}

	ready := make(chan struct{})
	dep := &testComponent{start: record("dependency-start"), stop: record("dependency-stop")}
	srv := &testComponent{start: record("server-start"), stop: record("server-stop")}
	application, err := New(
		WithSignals(),
		WithServers(srv),
		WithComponent(cx.NewKey[*testComponent]("dependency"), dep),
		WithOnStart(record("onStart")),
		WithOnStarted(func(ctx context.Context) error { _ = record("onStarted")(ctx); close(ready); return nil }),
		WithOnStopping(record("onStopping")),
		WithOnStop(record("onStop")),
	)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- application.Run(context.WithValue(t.Context(), ctxKey{}, "value")) }()
	<-ready

	if err := application.Run(t.Context()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("concurrent Run = %v, want ErrAlreadyRunning", err)
	}
	report, err := application.HealthCheck(t.Context())
	if err != nil || !report.Healthy {
		t.Fatalf("HealthCheck = (%+v, %v)", report, err)
	}
	application.Shutdown()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := []string{"onStart", "dependency-start", "server-start", "onStarted", "onStopping", "server-stop", "dependency-stop", "onStop"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if err := application.Run(t.Context()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("second Run = %v, want ErrAlreadyRun", err)
	}
}

func TestRunContextCancelRollbackAndRunnable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ready := make(chan struct{})
		application, err := New(
			WithSignals(),
			WithRunnable(cx.RunnerFunc(func(ctx context.Context) error {
				close(ready)
				<-ctx.Done()
				return ctx.Err()
			})),
		)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- application.Run(ctx) }()
		<-ready
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run after cancel = %v, want nil", err)
		}
	})

	runErr := errors.New("worker boom")
	stopErr := errors.New("stop boom")
	var logBuf bytes.Buffer
	application, err := New(
		WithSignals(),
		WithLogger(slog.New(slog.NewTextHandler(&logBuf, nil))),
		WithComponent(cx.NewKey[*testComponent]("res"), &testComponent{stop: func(context.Context) error { return stopErr }}),
		WithRunnable(cx.RunnerFunc(func(context.Context) error { return runErr })),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = application.Run(t.Context())
	if !errors.Is(err, runErr) || !errors.Is(err, stopErr) {
		t.Fatalf("Run error = %v, want joined runErr+stopErr", err)
	}
	if !strings.Contains(logBuf.String(), "shutdown completed with errors") {
		t.Fatalf("expected error log on shutdown failure, got %q", logBuf.String())
	}
}

func TestRunShutdownTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ready := make(chan struct{})
		resource := &testComponent{stop: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
		application, err := New(
			WithSignals(),
			WithShutdownTimeout(20*time.Millisecond),
			WithComponent(cx.TypeKey[*testComponent](), resource),
			WithOnStarted(func(context.Context) error { close(ready); return nil }),
		)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- application.Run(t.Context()) }()
		<-ready
		application.Shutdown()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run error = %v, want DeadlineExceeded", err)
		}
	})
}

func TestRunHTTPServerWithSignalsDisabled(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	defer ts.Close()

	ready := make(chan struct{})
	srv := &testComponent{
		start: func(context.Context) error { ts.Start(); return nil },
		stop:  func(context.Context) error { ts.Close(); return nil },
	}
	application, err := New(
		WithSignals(),
		WithServers(srv),
		WithOnStarted(func(context.Context) error { close(ready); return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- application.Run(t.Context()) }()
	<-ready
	application.Shutdown()
	if err := <-done; err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunSignalDuringStartup(t *testing.T) {
	const childEnv = "KIT_APP_SIGNAL_CHILD"
	if os.Getenv(childEnv) == "1" {
		application, err := New(WithSignals(os.Interrupt), WithOnStart(func(ctx context.Context) error {
			fmt.Println("startup-ready")
			<-ctx.Done()
			return ctx.Err()
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := application.Run(context.Background()); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRunSignalDuringStartup$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() || scanner.Text() != "startup-ready" {
		t.Fatalf("unexpected child output: %q (%v)", scanner.Text(), scanner.Err())
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child exited with error: %v", err)
	}
}

func TestShutdownBeforeRunAndTimeoutPropagation(t *testing.T) {
	application, err := New(WithSignals(), WithShutdownTimeout(15*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	application.Shutdown()
	if err := application.Run(t.Context()); err != nil {
		t.Fatalf("Run after early Shutdown = %v, want nil", err)
	}
}

type supervisedTestServer struct {
	done chan struct{}
	err  error
}

func (s *supervisedTestServer) Start(context.Context) error { return nil }
func (s *supervisedTestServer) Stop(context.Context) error  { return nil }
func (s *supervisedTestServer) Done() <-chan struct{}       { return s.done }
func (s *supervisedTestServer) Err() error                  { return s.err }

func TestWithServersUnexpectedExitTerminatesAppRun(t *testing.T) {
	crashErr := errors.New("listener died")
	srv := &supervisedTestServer{done: make(chan struct{})}
	var compStopped bool
	comp := &testComponent{stop: func(context.Context) error { compStopped = true; return nil }}

	application, err := New(
		WithSignals(),
		WithComponent(cx.NewKey[*testComponent]("dep"), comp),
		WithServers(srv),
	)
	if err != nil {
		t.Fatal(err)
	}

	runDone := make(chan error, 1)
	go func() { runDone <- application.Run(t.Context()) }()

	// Give Run time to enter Wait, then simulate unexpected server exit.
	time.Sleep(15 * time.Millisecond)
	srv.err = crashErr
	close(srv.done)

	select {
	case err := <-runDone:
		if !errors.Is(err, crashErr) {
			t.Fatalf("Run error = %v, want crashErr", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app.Run did not exit on supervised server crash")
	}
	if !compStopped {
		t.Fatal("companion component was not stopped on server crash")
	}
}
