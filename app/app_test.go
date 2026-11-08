package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/kochabx/kit/cx"
	kithttp "github.com/kochabx/kit/transport/http"
	"github.com/stretchr/testify/require"
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

func (c *testComponent) HealthCheck(context.Context) error {
	return nil
}

func mustNew(t *testing.T, options ...Option) *Application {
	t.Helper()
	value, err := New(options...)
	require.NoError(t, err)
	return value
}

func waitReady(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("startup did not complete")
	}
}

func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish")
		return nil
	}
}

func launch(t *testing.T, application *Application) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- application.Run() }()
	t.Cleanup(application.Shutdown)
	return done
}

func TestNewValidation(t *testing.T) {
	var nilServer *testComponent
	for _, tc := range []struct {
		name    string
		options []Option
	}{
		{"nil option", []Option{nil}},
		{"nil context", []Option{WithContext(nil)}},
		{"timeout", []Option{WithShutdownTimeout(0)}},
		{"nil container", []Option{WithContainer(nil)}},
		{"nil server", []Option{WithServer(nil)}},
		{"typed nil server", []Option{WithServer(nilServer)}},
		{"nil signal", []Option{WithSignals(nil)}},
		{"nil component", []Option{WithComponent(cx.NewKey[*testComponent]("component"), nilServer)}},
		{"empty key", []Option{WithComponent(cx.NewKey[int](""), 1)}},
		{"nil hook", []Option{WithOnStart(nil)}},
		{
			"external hook",
			[]Option{WithContainer(cx.New()), WithOnStart(func(context.Context) error { return nil })},
		},
		{"external server", []Option{WithServer(&testComponent{}), WithContainer(cx.New())}},
		{"external component", []Option{WithContainer(cx.New()), WithComponent(cx.NewKey[int]("value"), 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, err := New(tc.options...)
			require.Nil(t, app)
			require.ErrorIs(t, err, ErrInvalidOption)
		})
	}
	key := cx.NewKey[int]("duplicate")
	app, err := New(WithComponent(key, 1), WithComponent(key, 2))
	require.Nil(t, app)
	require.Error(t, err)
}

func TestRunOrderHooksAndContext(t *testing.T) {
	type key struct{}
	var order []string
	record := func(name string) func(context.Context) error {
		return func(ctx context.Context) error {
			if name == "server-stop" || name == "dependency-stop" {
				require.NoError(t, ctx.Err())
				require.Equal(t, "value", ctx.Value(key{}))
				_, hasDeadline := ctx.Deadline()
				require.True(t, hasDeadline)
			}
			order = append(order, name)
			return nil
		}
	}
	dependency := &testComponent{start: record("dependency-start"), stop: record("dependency-stop")}
	server := &testComponent{start: record("server-start"), stop: record("server-stop")}
	ready := make(chan struct{})
	app := mustNew(t, WithSignals(), WithContext(context.WithValue(context.Background(), key{}, "value")),
		WithServer(server), WithComponent(cx.NewKey[*testComponent]("dependency"), dependency),
		WithOnStart(record("onStart")),
		WithOnStarted(func(ctx context.Context) error { _ = record("onStarted")(ctx); close(ready); return nil }),
		WithOnStopping(record("onStopping")), WithOnStop(record("onStop")))
	done := launch(t, app)
	waitReady(t, ready)
	require.ErrorIs(t, app.Run(), ErrAlreadyRunning)
	report, err := app.HealthCheck(context.Background())
	require.NoError(t, err)
	require.True(t, report.Healthy)
	app.Shutdown()
	require.NoError(t, waitRun(t, done))
	require.Equal(
		t,
		[]string{
			"onStart",
			"dependency-start",
			"server-start",
			"onStarted",
			"onStopping",
			"server-stop",
			"dependency-stop",
			"onStop",
		},
		order,
	)
	require.ErrorIs(t, app.Run(), ErrAlreadyRun)
}

func TestRunContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	app := mustNew(t, WithSignals(), WithContext(ctx), WithOnStarted(func(context.Context) error {
		close(ready)
		return nil
	}))
	done := launch(t, app)
	waitReady(t, ready)
	cancel()
	require.NoError(t, waitRun(t, done))
}

func TestRunStartupCancellationAndRollback(t *testing.T) {
	entered := make(chan struct{})
	stopped := false
	dependency := &testComponent{stop: func(ctx context.Context) error { stopped = true; return ctx.Err() }}
	blocking := &testComponent{start: func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }}
	app := mustNew(t, WithSignals(), WithComponent(cx.NewKey[*testComponent]("dependency"), dependency), WithServer(blocking))
	done := launch(t, app)
	waitReady(t, entered)
	app.Shutdown()
	require.ErrorIs(t, waitRun(t, done), context.Canceled)
	require.True(t, stopped)
	require.ErrorIs(t, app.Run(), ErrAlreadyRun)
}

func TestRunSupervisedFailure(t *testing.T) {
	for _, failure := range []error{errors.New("worker failed"), context.Canceled} {
		runner := cx.MustRunner(cx.RunnerFunc(func(context.Context) error { return failure }))
		app := mustNew(t, WithSignals(), WithComponent(cx.NewKey[*cx.Runner]("worker"), runner))
		require.ErrorIs(t, app.Run(), failure)
		require.Equal(t, cx.StateStopped, app.Container().State())
	}
}

func TestRunShutdownFailure(t *testing.T) {
	failure := errors.New("stop failed")
	ready := make(chan struct{})
	app := mustNew(
		t,
		WithSignals(),
		WithComponent(cx.NewKey[*testComponent]("resource"), &testComponent{stop: func(context.Context) error {
			return failure
		}}),
		WithOnStarted(func(context.Context) error {
			close(ready)
			return nil
		}),
	)
	done := launch(t, app)
	waitReady(t, ready)
	app.Shutdown()
	require.ErrorIs(t, waitRun(t, done), failure)
}

func TestRunExternalContainer(t *testing.T) {
	ready := make(chan struct{})
	container := cx.New(cx.WithOnStarted(func(context.Context) error { close(ready); return nil }))
	container.MustSupply(cx.NewKey[*testComponent]("resource"), &testComponent{})
	app := mustNew(t, WithSignals(), WithContainer(container))
	require.Same(t, container, app.Container())
	done := launch(t, app)
	waitReady(t, ready)
	report, err := app.HealthCheck(context.Background())
	require.NoError(t, err)
	require.True(t, report.Healthy)
	_, err = New(WithContainer(container))
	require.ErrorIs(t, err, ErrInvalidOption)
	app.Shutdown()
	require.NoError(t, waitRun(t, done))
}

func TestSignalOptionsSnapshot(t *testing.T) {
	signals := []os.Signal{os.Interrupt}
	option := WithSignals(signals...)
	signals[0] = nil
	app := mustNew(t, option)
	require.True(t, reflect.DeepEqual([]os.Signal{os.Interrupt}, app.signals))
	app.Shutdown()
}

func TestRunHTTPServer(t *testing.T) {
	ready := make(chan struct{})
	app := mustNew(
		t,
		WithSignals(),
		WithServer(kithttp.NewServer(http.NotFoundHandler(), kithttp.WithAddr("127.0.0.1:0"))),
		WithOnStarted(func(context.Context) error {
			close(ready)
			return nil
		}),
	)
	done := launch(t, app)
	waitReady(t, ready)
	app.Shutdown()
	require.NoError(t, waitRun(t, done))
}

func TestRunSignalDuringStartup(t *testing.T) {
	const childEnv = "KIT_APP_SIGNAL_CHILD"
	if os.Getenv(childEnv) == "1" {
		app := mustNew(t, WithSignals(os.Interrupt), WithOnStart(func(ctx context.Context) error {
			fmt.Println("startup-ready")
			<-ctx.Done()
			return ctx.Err()
		}))
		require.ErrorIs(t, app.Run(), context.Canceled)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunSignalDuringStartup$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	scanner := bufio.NewScanner(output)
	require.True(t, scanner.Scan())
	require.Equal(t, "startup-ready", scanner.Text())
	require.NoError(t, cmd.Process.Signal(os.Interrupt))
	for scanner.Scan() {
	}
	require.NoError(t, scanner.Err())
	require.NoError(t, cmd.Wait())
}

func TestStartupFailureRollsBackBeforeServing(t *testing.T) {
	failure := errors.New("dependency startup failed")
	var order []string
	dependency := &testComponent{
		start: func(context.Context) error { order = append(order, "start"); return failure },
		stop:  func(context.Context) error { order = append(order, "rollback"); return nil },
	}
	server := &testComponent{start: func(context.Context) error { order = append(order, "serve"); return nil }}
	app := mustNew(t, WithSignals(), WithServer(server), WithComponent(cx.NewKey[*testComponent]("dependency"), dependency))
	require.ErrorIs(t, app.Run(), failure)
	require.Equal(t, []string{"start", "rollback"}, order)
	require.ErrorIs(t, app.Run(), ErrAlreadyRun)
}

func TestShutdownTimeout(t *testing.T) {
	ready := make(chan struct{})
	resource := &testComponent{stop: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	app := mustNew(
		t,
		WithSignals(),
		WithShutdownTimeout(20*time.Millisecond),
		WithComponent(cx.NewKey[*testComponent]("resource"), resource),
		WithOnStarted(func(context.Context) error {
			close(ready)
			return nil
		}),
	)
	done := launch(t, app)
	waitReady(t, ready)
	app.Shutdown()
	require.ErrorIs(t, waitRun(t, done), context.DeadlineExceeded)
}

func TestSupervisedAndShutdownErrorsAreJoined(t *testing.T) {
	runErr := errors.New("worker failed")
	stopErr := errors.New("cleanup failed")
	runner := cx.MustRunner(cx.RunnerFunc(func(context.Context) error { return runErr }))
	app := mustNew(
		t,
		WithSignals(),
		WithComponent(cx.NewKey[*testComponent]("resource"), &testComponent{stop: func(context.Context) error {
			return stopErr
		}}),
		WithComponent(cx.NewKey[*cx.Runner]("runner"), runner),
	)
	err := app.Run()
	require.ErrorIs(t, err, runErr)
	require.ErrorIs(t, err, stopErr)
}
