package cx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestTypedKeysAndDependencyOrder(t *testing.T) {
	c := New()
	configKey := NewKey[string]("config")
	dbKey := NewKey[*testComponent]("db")
	serviceKey := NewKey[*testComponent]("service")
	var order []string

	MustSupply(c, configKey, "dsn")
	MustProvide(c, dbKey, func(c *Container) (*testComponent, error) {
		require.Equal(t, "dsn", MustGet(c, configKey))
		return &testComponent{
			order: &order,
			name:  "db",
		}, nil
	})
	MustProvide(c, serviceKey, func(c *Container) (*testComponent, error) {
		_ = MustGet(c, dbKey)
		return &testComponent{
			order: &order,
			name:  "service",
		}, nil
	})

	require.NoError(t, c.Start(context.Background()))
	assert.Equal(t, []string{"start:db", "start:service"}, order)

	got, err := Get(c, serviceKey)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, []string{"start:db", "start:service", "stop:service", "stop:db"}, order)
}

func TestCircularDependency(t *testing.T) {
	c := New()
	a := NewKey[int]("a")
	b := NewKey[int]("b")

	MustProvide(c, a, func(c *Container) (int, error) {
		_, err := Get(c, b)
		return 0, err
	})
	MustProvide(c, b, func(c *Container) (int, error) {
		_, err := Get(c, a)
		return 0, err
	})

	err := c.Start(context.Background())
	assert.ErrorIs(t, err, ErrCircularDependency)
	assert.Equal(t, StateFailed, c.State())
}

func TestExplicitDependenciesControlLifecycleOrder(t *testing.T) {
	c := New()
	keyA := NewKey[*testComponent]("a")
	keyB := NewKey[*testComponent]("b")
	keyC := NewKey[*testComponent]("c")
	var order []string

	MustSupply(c, keyA, &testComponent{order: &order, name: "a"}, DependsOn(keyB))
	MustSupply(c, keyB, &testComponent{order: &order, name: "b"}, DependsOn(keyC))
	MustSupply(c, keyC, &testComponent{order: &order, name: "c"})

	require.NoError(t, c.Start(context.Background()))
	assert.Equal(t, []string{"start:c", "start:b", "start:a"}, order)
	assert.Equal(t, []string{"b"}, c.DependencyGraph()["a"])
	assert.Equal(t, []string{"c"}, c.DependencyGraph()["b"])

	require.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, []string{
		"start:c", "start:b", "start:a",
		"stop:a", "stop:b", "stop:c",
	}, order)
}

func TestExplicitDependencyCycle(t *testing.T) {
	c := New()
	keyA := NewKey[int]("a")
	keyB := NewKey[int]("b")

	MustSupply(c, keyA, 1, DependsOn(keyB))
	MustSupply(c, keyB, 2, DependsOn(keyA))

	err := c.Start(context.Background())
	assert.ErrorIs(t, err, ErrCircularDependency)
}

func TestStartFailureRollsBackFailingComponentAndAggregates(t *testing.T) {
	startErr := errors.New("start")
	stopErr := errors.New("rollback")
	var order []string
	c := New()
	key := NewKey[*testComponent]("bad")
	MustSupply(c, key, &testComponent{
		order:    &order,
		name:     "bad",
		startErr: startErr,
		stopErr:  stopErr,
	})

	err := c.Start(context.Background())
	assert.ErrorIs(t, err, startErr)
	assert.ErrorIs(t, err, stopErr)
	assert.Equal(t, []string{"start:bad", "stop:bad"}, order)
	assert.Equal(t, StateStopFailed, c.State())
}

func TestRollbackUsesIndependentOverallTimeout(t *testing.T) {
	startErr := errors.New("start failed")
	c := New(
		WithRollbackTimeout(5*time.Millisecond),
		WithComponentStopTimeout(time.Second),
	)
	key := NewKey[*lifecycleComponent]("component")
	MustSupply(c, key, &lifecycleComponent{
		start: func(context.Context) error { return startErr },
		stop: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	started := time.Now()
	err := c.Start(context.Background())
	assert.ErrorIs(t, err, startErr)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 100*time.Millisecond)
	assert.Equal(t, StateStopFailed, c.State())
}

func TestStopFailureRetainsInstanceAndCanRetry(t *testing.T) {
	var order []string
	c := New()
	key := NewKey[*testComponent]("component")
	component := &testComponent{
		order:   &order,
		name:    "component",
		stopErr: errors.New("stop"),
	}
	MustSupply(c, key, component)

	require.NoError(t, c.Start(context.Background()))
	require.Error(t, c.Stop(context.Background()))
	assert.Equal(t, StateStopFailed, c.State())

	got, err := Get(c, key)
	require.NoError(t, err)
	assert.Same(t, component, got)

	component.stopErr = nil
	require.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, StateStopped, c.State())
}

func TestSuccessfulStopHooksAreNotRepeated(t *testing.T) {
	stopErr := errors.New("hook failed")
	firstCalls := 0
	secondCalls := 0
	c := New(
		WithOnStop(func(context.Context) error {
			firstCalls++
			return nil
		}),
		WithOnStop(func(context.Context) error {
			secondCalls++
			if secondCalls == 1 {
				return stopErr
			}
			return nil
		}),
	)

	require.NoError(t, c.Start(context.Background()))
	assert.ErrorIs(t, c.Stop(context.Background()), stopErr)
	require.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, 1, firstCalls)
	assert.Equal(t, 2, secondCalls)
}

func TestHealthCheckStatusesAndState(t *testing.T) {
	c := New(WithHealthTimeout(5 * time.Millisecond))
	healthy := NewKey[*testComponent]("healthy")
	bad := NewKey[*testComponent]("bad")
	plain := NewKey[int]("plain")
	var order []string

	MustSupply(c, healthy, &testComponent{order: &order, name: "healthy"})
	MustSupply(c, bad, &testComponent{
		order:     &order,
		name:      "bad",
		healthErr: errors.New("bad"),
	})
	MustSupply(c, plain, 1)

	_, err := c.HealthCheck(context.Background())
	assert.ErrorIs(t, err, ErrInvalidState)

	require.NoError(t, c.Start(context.Background()))
	report, err := c.HealthCheck(context.Background())
	require.NoError(t, err)
	assert.False(t, report.Healthy)
	assert.Equal(t, HealthHealthy, report.Components[0].Status)
	assert.Equal(t, HealthUnhealthy, report.Components[1].Status)
	assert.Equal(t, HealthSkipped, report.Components[2].Status)
	require.NoError(t, c.Stop(context.Background()))
}

func TestHealthCheckBoundsComponentIgnoringContext(t *testing.T) {
	c := New(WithHealthTimeout(5 * time.Millisecond))
	key := NewKey[HealthChecker]("blocked")
	release := make(chan struct{})
	blocked := HealthCheckerFunc(func(context.Context) error {
		<-release
		return nil
	})
	MustSupply(c, key, HealthChecker(blocked))

	require.NoError(t, c.Start(context.Background()))
	started := time.Now()
	report, err := c.HealthCheck(context.Background())
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 100*time.Millisecond)
	assert.Equal(t, HealthTimeout, report.Components[0].Status)
	close(release)
}

func TestHealthCheckReusesTimedOutFlight(t *testing.T) {
	c := New(WithHealthTimeout(time.Millisecond))
	key := NewKey[HealthChecker]("blocked")
	release := make(chan struct{})
	var calls atomic.Int32
	checker := HealthCheckerFunc(func(context.Context) error {
		calls.Add(1)
		<-release
		return nil
	})
	MustSupply(c, key, HealthChecker(checker))
	require.NoError(t, c.Start(context.Background()))

	for range 3 {
		report, err := c.HealthCheck(context.Background())
		require.NoError(t, err)
		assert.Equal(t, HealthTimeout, report.Components[0].Status)
	}
	assert.Equal(t, int32(1), calls.Load())
	close(release)
}

func TestWaitSupervisedExit(t *testing.T) {
	c := New()
	key := NewKey[*Runner]("worker")
	runErr := errors.New("worker failed")
	runner := MustRunner(RunnerFunc(func(context.Context) error {
		return runErr
	}))
	MustSupply(c, key, runner)

	require.NoError(t, c.Start(context.Background()))
	err := c.Wait(context.Background())
	assert.ErrorIs(t, err, runErr)
	require.NoError(t, c.Stop(context.Background()))
}

func TestWaitSharesSupervisorResult(t *testing.T) {
	c := New()
	key := NewKey[*Runner]("worker")
	runErr := errors.New("worker failed")
	release := make(chan struct{})
	runner := MustRunner(RunnerFunc(func(context.Context) error {
		<-release
		return runErr
	}))
	MustSupply(c, key, runner)
	require.NoError(t, c.Start(context.Background()))

	results := make(chan error, 2)
	go func() { results <- c.Wait(context.Background()) }()
	go func() { results <- c.Wait(context.Background()) }()
	close(release)

	assert.ErrorIs(t, <-results, runErr)
	assert.ErrorIs(t, <-results, runErr)
	require.NoError(t, c.Stop(context.Background()))
}

func TestRunnerStopTimeoutAndRetry(t *testing.T) {
	release := make(chan struct{})
	runner := MustRunner(RunnerFunc(func(context.Context) error {
		<-release
		return nil
	}))
	require.NoError(t, runner.Start(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	assert.ErrorIs(t, runner.Stop(ctx), context.DeadlineExceeded)
	assert.ErrorIs(t, runner.Start(context.Background()), ErrRunnerAlreadyStarted)

	close(release)
	require.NoError(t, runner.Stop(context.Background()))
	require.NoError(t, runner.Start(context.Background()))
}

func TestRunnerPanicIsSupervised(t *testing.T) {
	runner := MustRunner(RunnerFunc(func(context.Context) error {
		panic("boom")
	}))
	require.NoError(t, runner.Start(context.Background()))

	<-runner.Done()
	assert.ErrorIs(t, runner.Err(), ErrRunnerPanic)
	require.NoError(t, runner.Stop(context.Background()))
}

func TestNewRunnerRejectsTypedNil(t *testing.T) {
	var runnable *typedNilRunnable
	runner, err := NewRunner(runnable)
	assert.Nil(t, runner)
	assert.ErrorIs(t, err, ErrNilRunnable)
}

type typedNilRunnable struct{}

func (*typedNilRunnable) Run(context.Context) error { return nil }

type lifecycleComponent struct {
	start func(context.Context) error
	stop  func(context.Context) error
}

func (c *lifecycleComponent) Start(ctx context.Context) error {
	return c.start(ctx)
}

func (c *lifecycleComponent) Stop(ctx context.Context) error {
	return c.stop(ctx)
}

type HealthCheckerFunc func(context.Context) error

func (f HealthCheckerFunc) HealthCheck(ctx context.Context) error {
	return f(ctx)
}
