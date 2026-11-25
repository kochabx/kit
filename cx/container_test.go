package cx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type testComponent struct {
	mu        sync.Mutex
	order     *[]string
	name      string
	startErr  error
	stopErr   error
	healthErr error
}

func (c *testComponent) Start(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.order = append(*c.order, "start:"+c.name)
	return c.startErr
}

func (c *testComponent) Stop(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.order = append(*c.order, "stop:"+c.name)
	return c.stopErr
}

func (c *testComponent) HealthCheck(context.Context) error {
	return c.healthErr
}

type lifecycleComponent struct {
	start func(context.Context) error
	stop  func(context.Context) error
}

func (c *lifecycleComponent) Start(ctx context.Context) error { return c.start(ctx) }
func (c *lifecycleComponent) Stop(ctx context.Context) error  { return c.stop(ctx) }

type healthCheckerFunc func(context.Context) error

func (f healthCheckerFunc) HealthCheck(ctx context.Context) error { return f(ctx) }

type typedNilRunnable struct{}

func (*typedNilRunnable) Run(context.Context) error { return nil }

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

func TestTypeKeyAndTypeValidation(t *testing.T) {
	c := New()
	k := TypeKey[*testComponent]()
	var order []string
	comp := &testComponent{order: &order, name: "singleton"}
	c.MustSupply(k, comp)

	wrongKey := NewKey[string](k.String())
	if c.Has(wrongKey) {
		t.Fatal("Has must return false on type mismatch")
	}
	if _, err := c.Get(wrongKey); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("Get with wrong type = %v, want ErrTypeMismatch", err)
	}

	called := false
	intKey := NewKey[int]("counter")
	c.MustProvide(intKey, func(*Container) (int, error) {
		called = true
		return 7, nil
	})
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !called {
		t.Fatal("int provider was not built")
	}
	got, err := c.Get(k)
	if err != nil || got != comp {
		t.Fatalf("Get(TypeKey) = (%v, %v), want %v", got, err, comp)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestTypedKeysDependencyOrderAndConcurrentGet(t *testing.T) {
	c := New()
	cfgKey := NewKey[string]("config")
	dbKey := TypeKey[*testComponent]()
	svcKey := NewKey[*testComponent]("service")
	var order []string

	c.MustSupply(cfgKey, "dsn")
	c.MustProvide(dbKey, func(sc *Container) (*testComponent, error) {
		if sc.MustGet(cfgKey) != "dsn" {
			return nil, errors.New("unexpected dsn")
		}
		return &testComponent{order: &order, name: "db"}, nil
	})
	c.MustProvide(svcKey, func(sc *Container) (*testComponent, error) {
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				_ = sc.MustGet(dbKey)
				_ = sc.MustGet(cfgKey)
			})
		}
		wg.Wait()
		return &testComponent{order: &order, name: "service"}, nil
	})

	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	wantStart := []string{"start:db", "start:service"}
	if !slices.Equal(order, wantStart) {
		t.Fatalf("order = %v, want %v", order, wantStart)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	wantAll := []string{"start:db", "start:service", "stop:service", "stop:db"}
	if !slices.Equal(order, wantAll) {
		t.Fatalf("order = %v, want %v", order, wantAll)
	}
}

func TestCircularAndExplicitDependencies(t *testing.T) {
	c := New()
	a := NewKey[int]("a")
	b := NewKey[int]("b")
	c.MustProvide(a, func(sc *Container) (int, error) { _, err := sc.Get(b); return 0, err })
	c.MustProvide(b, func(sc *Container) (int, error) { _, err := sc.Get(a); return 0, err })

	if err := c.Start(t.Context()); !errors.Is(err, ErrCircularDependency) {
		t.Fatalf("Start err = %v, want ErrCircularDependency", err)
	}
	if c.State() != StateFailed {
		t.Fatalf("state = %v, want StateFailed", c.State())
	}

	c2 := New()
	kA, kB, kC := NewKey[*testComponent]("a"), NewKey[*testComponent]("b"), NewKey[*testComponent]("c")
	var order []string
	c2.MustSupply(kA, &testComponent{order: &order, name: "a"}, DependsOn(kB))
	c2.MustSupply(kB, &testComponent{order: &order, name: "b"}, DependsOn(kC))
	c2.MustSupply(kC, &testComponent{order: &order, name: "c"})
	if err := c2.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if want := []string{"start:c", "start:b", "start:a"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if err := c2.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestStartFailureRollbackAndRetryableStop(t *testing.T) {
	startErr := errors.New("start")
	stopErr := errors.New("rollback")
	var order []string
	c := New()
	c.MustSupply(NewKey[*testComponent]("bad"), &testComponent{
		order: &order, name: "bad", startErr: startErr, stopErr: stopErr,
	})
	err := c.Start(t.Context())
	if !errors.Is(err, startErr) || !errors.Is(err, stopErr) || c.State() != StateStopFailed {
		t.Fatalf("unexpected start failure state: err=%v state=%v", err, c.State())
	}

	synctest.Test(t, func(t *testing.T) {
		c2 := New(WithRollbackTimeout(5*time.Millisecond), WithComponentStopTimeout(time.Second))
		c2.MustSupply(TypeKey[*lifecycleComponent](), &lifecycleComponent{
			start: func(context.Context) error { return startErr },
			stop:  func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		})
		start := time.Now()
		err := c2.Start(t.Context())
		if !errors.Is(err, startErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected rollback error: %v", err)
		}
		if elapsed := time.Since(start); elapsed != 5*time.Millisecond {
			t.Fatalf("elapsed = %v, want 5ms", elapsed)
		}
	})

	comp := &testComponent{order: new([]string), name: "retry", stopErr: errors.New("stop")}
	c3 := New()
	c3.MustSupply(TypeKey[*testComponent](), comp)
	if err := c3.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := c3.Stop(t.Context()); err == nil || c3.State() != StateStopFailed {
		t.Fatalf("expected StateStopFailed, got state=%v err=%v", c3.State(), err)
	}
	comp.stopErr = nil
	if err := c3.Stop(t.Context()); err != nil || c3.State() != StateStopped {
		t.Fatalf("retry Stop failed: err=%v state=%v", err, c3.State())
	}
}

func TestStopHooksNotRepeated(t *testing.T) {
	stopErr := errors.New("hook failed")
	var first, second int
	c := New(
		WithOnStop(func(context.Context) error { first++; return nil }),
		WithOnStop(func(context.Context) error {
			second++
			if second == 1 {
				return stopErr
			}
			return nil
		}),
	)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(t.Context()); !errors.Is(err, stopErr) {
		t.Fatalf("first Stop = %v, want stopErr", err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("second Stop = %v, want nil", err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("hook counts = (%d, %d), want (1, 2)", first, second)
	}
}

func TestHealthCheckTimeoutCappedAndRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New(WithHealthTimeout(5 * time.Millisecond))
		release := make(chan struct{})
		var calls atomic.Int32
		checker := healthCheckerFunc(func(context.Context) error {
			if calls.Add(1) == 1 {
				<-release
			}
			return nil
		})
		c.MustSupply(NewKey[HealthChecker]("blocked"), HealthChecker(checker))
		c.MustSupply(NewKey[int]("plain"), 1)

		if _, err := c.HealthCheck(t.Context()); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("HealthCheck before Start = %v, want ErrInvalidState", err)
		}
		if err := c.Start(t.Context()); err != nil {
			t.Fatal(err)
		}

		// Timed-out calls must share the in-flight probe (concurrency == 1).
		for range 3 {
			report, err := c.HealthCheck(t.Context())
			if err != nil {
				t.Fatalf("HealthCheck failed: %v", err)
			}
			if report.Healthy || report.Components[0].Status != HealthTimeout || report.Components[1].Status != HealthSkipped {
				t.Fatalf("unexpected timeout report: %+v", report)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("probe calls while stuck = %d, want 1", got)
		}

		// Once the stuck probe finishes, flight clears and subsequent HealthCheck recovers.
		close(release)
		synctest.Wait()

		report, err := c.HealthCheck(t.Context())
		if err != nil {
			t.Fatalf("recovery HealthCheck failed: %v", err)
		}
		if !report.Healthy || report.Components[0].Status != HealthHealthy || calls.Load() != 2 {
			t.Fatalf("expected recovery, got report=%+v calls=%d", report, calls.Load())
		}
		if err := c.Stop(t.Context()); err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
	})
}

func TestWaitAndRunnerSupervisionZeroLeaks(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() { assertNoGoroutineLeaks(t, baseline) })

	runErr := errors.New("worker failed")
	release := make(chan struct{})
	c := New()
	runner := MustRunner(RunnerFunc(func(context.Context) error {
		<-release
		return runErr
	}))
	c.MustSupply(NewKey[*Runner]("worker"), runner)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	go func() { results <- c.Wait(t.Context()) }()
	go func() { results <- c.Wait(t.Context()) }()
	close(release)

	for range 2 {
		if err := <-results; !errors.Is(err, runErr) {
			t.Fatalf("Wait = %v, want %v", err, runErr)
		}
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	var nilRunnable *typedNilRunnable
	if _, err := NewRunner(nilRunnable); !errors.Is(err, ErrNilRunnable) {
		t.Fatalf("NewRunner(typed nil) = %v, want ErrNilRunnable", err)
	}

	panicRunner := MustRunner(RunnerFunc(func(context.Context) error { panic("boom") }))
	if err := panicRunner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-panicRunner.Done()
	if !errors.Is(panicRunner.Err(), ErrRunnerPanic) {
		t.Fatalf("Err() = %v, want ErrRunnerPanic", panicRunner.Err())
	}
	if err := panicRunner.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestSingleflightBuildNilInterfaceGraphAndOnStarted(t *testing.T) {
	// 1. Concurrent Get on an unbuilt dependency constructs it exactly once.
	c := New()
	var constructs atomic.Int32
	var stops atomic.Int32
	depKey := NewKey[*lifecycleComponent]("dep")
	svcKey := NewKey[string]("svc")
	c.MustProvide(depKey, func(*Container) (*lifecycleComponent, error) {
		constructs.Add(1)
		return &lifecycleComponent{
			start: func(context.Context) error { return nil },
			stop:  func(context.Context) error { stops.Add(1); return nil },
		}, nil
	})
	c.MustProvide(svcKey, func(sc *Container) (string, error) {
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 8 {
			wg.Go(func() {
				<-start
				_ = sc.MustGet(depKey)
			})
		}
		close(start)
		wg.Wait()
		return "ok", nil
	})
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if constructs.Load() != 1 {
		t.Fatalf("expected 1 constructor invocation, got %d", constructs.Load())
	}
	// DependencyGraph records dependencies even when dep was already built before/during Get.
	if graph := c.DependencyGraph(); !slices.Contains(graph["svc"], "dep") {
		t.Fatalf("expected dep in svc DependencyGraph, got %+v", graph)
	}
	if err := c.Stop(t.Context()); err != nil || stops.Load() != 1 {
		t.Fatalf("Stop = %v, stops = %d", err, stops.Load())
	}

	// 2. Nil interface component Get succeeds without ErrTypeMismatch; Wait returns nil on Stop.
	synctest.Test(t, func(t *testing.T) {
		var nilCloser io.Closer
		closerKey := NewKey[io.Closer]("opt_closer")
		var hookState State
		var c2 *Container
		c2 = New(WithOnStarted(func(context.Context) error {
			hookState = c2.State()
			return nil
		}))
		c2.MustSupply(closerKey, nilCloser)
		if err := c2.Start(t.Context()); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		if hookState != StateRunning {
			t.Fatalf("onStarted saw state=%v, want StateRunning", hookState)
		}
		gotCloser, err := c2.Get(closerKey)
		if err != nil || gotCloser != nil {
			t.Fatalf("Get(nil closer) = (%v, %v), want (nil, nil)", gotCloser, err)
		}
		waitErr := make(chan error, 1)
		go func() { waitErr <- c2.Wait(t.Context()) }()
		synctest.Wait()
		if err := c2.Stop(t.Context()); err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
		if err := <-waitErr; err != nil {
			t.Fatalf("Wait after graceful Stop = %v, want nil", err)
		}
	})
}

func TestStartOnStartedConcurrentStopNoDoubleCleanup(t *testing.T) {
	hookErr := errors.New("onStarted aborted")
	var stops atomic.Int32
	inHook := make(chan struct{})
	stopDone := make(chan error, 1)

	var c *Container
	c = New(
		WithOnStarted(func(context.Context) error {
			close(inHook)
			<-time.After(10 * time.Millisecond)
			return hookErr
		}),
	)
	c.MustSupply(TypeKey[*lifecycleComponent](), &lifecycleComponent{
		start: func(context.Context) error { return nil },
		stop: func(context.Context) error {
			stops.Add(1)
			return nil
		},
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- c.Start(t.Context())
	}()

	<-inHook
	go func() {
		stopDone <- c.Stop(t.Context())
	}()

	startErr := <-startDone
	stopErr := <-stopDone
	if !errors.Is(startErr, hookErr) {
		t.Fatalf("Start err = %v, want hookErr", startErr)
	}
	if stopErr != nil {
		t.Fatalf("Stop err = %v, want nil", stopErr)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("component stop count = %d, want 1", got)
	}
	if state := c.State(); state != StateStopped {
		t.Fatalf("final state = %v, want StateStopped", state)
	}
}

func TestConcurrentBuildCircularDependencyAndSharedDiamond(t *testing.T) {
	// 1. Cross-goroutine A -> B and B -> A cycle during Start must fail with ErrCircularDependency without deadlocking.
	c := New()
	kRoot := NewKey[string]("Root")
	kA := NewKey[string]("A")
	kB := NewKey[string]("B")
	entered := make(chan struct{}, 2)
	proceed := make(chan struct{})

	c.MustProvide(kRoot, func(sc *Container) (string, error) {
		var (
			wg   sync.WaitGroup
			errA error
			errB error
		)
		wg.Go(func() { _, errA = sc.Get(kA) })
		wg.Go(func() { _, errB = sc.Get(kB) })
		wg.Wait()
		return "", errors.Join(errA, errB)
	})
	c.MustProvide(kA, func(sc *Container) (string, error) {
		entered <- struct{}{}
		<-proceed
		if _, err := sc.Get(kB); err != nil {
			return "", err
		}
		return "a", nil
	})
	c.MustProvide(kB, func(sc *Container) (string, error) {
		entered <- struct{}{}
		<-proceed
		if _, err := sc.Get(kA); err != nil {
			return "", err
		}
		return "b", nil
	})

	startDone := make(chan error, 1)
	go func() { startDone <- c.Start(t.Context()) }()
	<-entered
	<-entered
	close(proceed)

	select {
	case err := <-startDone:
		if !errors.Is(err, ErrCircularDependency) {
			t.Fatalf("expected ErrCircularDependency, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent circular Start deadlocked")
	}

	// 2. Non-cyclic concurrent diamond shares a single constructor call for shared component C.
	var cCalls atomic.Int32
	c2 := New()
	kRoot2 := NewKey[int]("Root2")
	kShared := NewKey[int]("C")
	kLeft := NewKey[int]("Left")
	kRight := NewKey[int]("Right")
	sharedReady := make(chan struct{})
	c2.MustProvide(kShared, func(*Container) (int, error) {
		cCalls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return 42, nil
	})
	c2.MustProvide(kLeft, func(sc *Container) (int, error) {
		close(sharedReady)
		return sc.Get(kShared)
	})
	c2.MustProvide(kRight, func(sc *Container) (int, error) {
		<-sharedReady
		return sc.Get(kShared)
	})
	c2.MustProvide(kRoot2, func(sc *Container) (int, error) {
		var (
			wg   sync.WaitGroup
			errL error
			errR error
		)
		wg.Go(func() { _, errL = sc.Get(kLeft) })
		wg.Go(func() { _, errR = sc.Get(kRight) })
		wg.Wait()
		return 1, errors.Join(errL, errR)
	})

	if err := c2.Start(t.Context()); err != nil {
		t.Fatalf("diamond Start failed: %v", err)
	}
	if err := c2.Stop(t.Context()); err != nil {
		t.Fatalf("diamond Stop failed: %v", err)
	}
	if got := cCalls.Load(); got != 1 {
		t.Fatalf("shared constructor C called %d times, want 1", got)
	}
}

func TestStartRollbackWaitsAndStopsConcurrentGetInstance(t *testing.T) {
	c := New()
	kRoot := NewKey[int]("root")
	kSlow := NewKey[*lifecycleComponent]("slow")

	slowEntered := make(chan struct{})
	var stops atomic.Int32
	c.MustProvide(kSlow, func(*Container) (*lifecycleComponent, error) {
		close(slowEntered)
		time.Sleep(25 * time.Millisecond)
		return &lifecycleComponent{
			start: func(context.Context) error { return nil },
			stop:  func(context.Context) error { stops.Add(1); return nil },
		}, nil
	})
	c.MustProvide(kRoot, func(sc *Container) (int, error) {
		go func() {
			_, _ = sc.Get(kSlow)
		}()
		<-slowEntered
		return 0, errors.New("boom")
	})

	err := c.Start(t.Context())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom start error, got %v", err)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("expected concurrently constructed component stopped once, got %d", got)
	}
}

func TestStartRollbackPreservesConcurrentGetStopFailureForRetry(t *testing.T) {
	c := New()
	kRoot := NewKey[int]("root")
	kSlow := NewKey[*lifecycleComponent]("slow")

	slowEntered := make(chan struct{})
	stopErr := errors.New("transient stop error")
	var stopCalls atomic.Int32
	c.MustProvide(kSlow, func(*Container) (*lifecycleComponent, error) {
		close(slowEntered)
		time.Sleep(25 * time.Millisecond)
		return &lifecycleComponent{
			start: func(context.Context) error { return nil },
			stop: func(context.Context) error {
				if stopCalls.Add(1) == 1 {
					return stopErr
				}
				return nil
			},
		}, nil
	})
	c.MustProvide(kRoot, func(sc *Container) (int, error) {
		go func() {
			_, _ = sc.Get(kSlow)
		}()
		<-slowEntered
		return 0, errors.New("root failed")
	})

	err := c.Start(t.Context())
	if !errors.Is(err, stopErr) {
		t.Fatalf("Start rollback must surface concurrent component Stop error, got %v", err)
	}
	if state := c.State(); state != StateStopFailed {
		t.Fatalf("State after failed rollback Stop = %v, want StateStopFailed", state)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop retry failed: %v", err)
	}
	if got := stopCalls.Load(); got != 2 {
		t.Fatalf("expected Stop retried cleanly (2 calls), got %d", got)
	}
	if state := c.State(); state != StateStopped {
		t.Fatalf("State after retry Stop = %v, want StateStopped", state)
	}
}

func TestStartOnStartedConcurrentRestartPreservesSecondGeneration(t *testing.T) {
	hookErr := errors.New("round1 onStarted failed")
	var hookCalls atomic.Int32
	var stops atomic.Int32
	inHook1 := make(chan struct{})
	releaseHook1 := make(chan struct{})

	var instanceGen atomic.Int32
	c := New(WithOnStarted(func(context.Context) error {
		if hookCalls.Add(1) == 1 {
			close(inHook1)
			<-releaseHook1
			return hookErr
		}
		return nil
	}))
	c.MustProvide(TypeKey[*lifecycleComponent](), func(*Container) (*lifecycleComponent, error) {
		instanceGen.Add(1)
		return &lifecycleComponent{
			start: func(context.Context) error { return nil },
			stop: func(context.Context) error {
				stops.Add(1)
				return nil
			},
		}, nil
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- c.Start(t.Context())
	}()

	<-inHook1
	restartDone := make(chan error, 1)
	go func() {
		restartDone <- c.Restart(t.Context())
	}()

	if err := <-restartDone; err != nil {
		close(releaseHook1)
		t.Fatalf("Restart failed: %v", err)
	}
	if got := c.State(); got != StateRunning {
		close(releaseHook1)
		t.Fatalf("State after Restart = %v, want StateRunning", got)
	}

	// Unblock round-1 onStarted so it returns hookErr while round-2 is running.
	close(releaseHook1)
	if err := <-startDone; !errors.Is(err, hookErr) {
		t.Fatalf("round-1 Start = %v, want hookErr", err)
	}

	// Round-2 must remain running and untouched by round-1's late onStarted failure.
	if got := c.State(); got != StateRunning {
		t.Fatalf("State after round-1 onStarted error = %v, want StateRunning", got)
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("stops = %d, want 1 (only round-1 stopped)", got)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("final Stop failed: %v", err)
	}
	if got := stops.Load(); got != 2 || c.State() != StateStopped {
		t.Fatalf("after final Stop: stops=%d state=%v, want 2 and StateStopped", got, c.State())
	}
}

func TestStartRollbackTimeoutThenRestartIsolatesOldConstructor(t *testing.T) {
	c := New(WithRollbackTimeout(20 * time.Millisecond))
	kRoot := NewKey[int]("root")
	kSlow := NewKey[*lifecycleComponent]("slow")

	var round atomic.Int32
	slowEntered := make(chan struct{})
	releaseHung := make(chan struct{})
	var r1Stops atomic.Int32
	var r2Stops atomic.Int32

	var rootCalls atomic.Int32
	c.MustProvide(kRoot, func(sc *Container) (int, error) {
		if rootCalls.Add(1) == 1 {
			go func() {
				_, _ = sc.Get(kSlow)
			}()
			<-slowEntered
			return 0, errors.New("round1 failed")
		}
		return 1, nil
	})
	c.MustProvide(kSlow, func(*Container) (*lifecycleComponent, error) {
		r := round.Add(1)
		if r == 1 {
			close(slowEntered)
			<-releaseHung
			return &lifecycleComponent{
				start: func(context.Context) error { return nil },
				stop:  func(context.Context) error { r1Stops.Add(1); return nil },
			}, nil
		}
		return &lifecycleComponent{
			start: func(context.Context) error { return nil },
			stop:  func(context.Context) error { r2Stops.Add(1); return nil },
		}, nil
	})

	if err := c.Start(t.Context()); err == nil {
		close(releaseHung)
		t.Fatal("expected round1 Start failure")
	}

	// Start round 2 immediately while round 1's hung constructor is still blocked.
	if err := c.Start(t.Context()); err != nil {
		close(releaseHung)
		t.Fatalf("round2 Start failed: %v", err)
	}
	if state := c.State(); state != StateRunning {
		close(releaseHung)
		t.Fatalf("State after round2 Start = %v, want StateRunning", state)
	}

	// Unblock round 1's stale constructor: it must self-stop its stale instance without touching round 2.
	close(releaseHung)
	for range 200 {
		if r1Stops.Load() == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if r1Stops.Load() != 1 || r2Stops.Load() != 0 || c.State() != StateRunning {
		t.Fatalf("stale constructor corrupted round2: r1Stops=%d r2Stops=%d state=%v", r1Stops.Load(), r2Stops.Load(), c.State())
	}

	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("final Stop failed: %v", err)
	}
	if r2Stops.Load() != 1 || c.State() != StateStopped {
		t.Fatalf("after final Stop: r2Stops=%d state=%v, want 1 and StateStopped", r2Stops.Load(), c.State())
	}
}
