package cx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type testConfig struct {
	DSN string
}

type testDB struct {
	cfg     *testConfig
	started bool
	stopped bool
}

func (d *testDB) Start(context.Context) error { d.started = true; return nil }
func (d *testDB) Stop(context.Context) error  { d.stopped = true; return nil }

type testService struct {
	db      *testDB
	healthy bool
	started bool
	stopped bool
}

func (s *testService) Start(context.Context) error { s.started = true; return nil }
func (s *testService) Stop(context.Context) error  { s.stopped = true; return nil }
func (s *testService) Check(context.Context) error {
	if !s.healthy {
		return errors.New("unhealthy")
	}
	return nil
}

// failStarter always fails on Start.
type failStarter struct{ stopped bool }

func (f *failStarter) Start(context.Context) error { return errors.New("start failed") }
func (f *failStarter) Stop(context.Context) error  { f.stopped = true; return nil }

// orderRecorder records Start/Stop call order.
type orderRecorder struct {
	key    string
	record *[]string
}

func (o *orderRecorder) Start(context.Context) error {
	*o.record = append(*o.record, "start:"+o.key)
	return nil
}
func (o *orderRecorder) Stop(context.Context) error {
	*o.record = append(*o.record, "stop:"+o.key)
	return nil
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestProvide_Get_Basic(t *testing.T) {
	c := New()
	err := Provide(c, "config", func(_ *Container) (*testConfig, error) {
		return &testConfig{DSN: "postgres://localhost"}, nil
	})
	require.NoError(t, err)
	require.NoError(t, c.Start(context.Background()))

	cfg, err := Get[*testConfig](c, "config")
	require.NoError(t, err)
	assert.Equal(t, "postgres://localhost", cfg.DSN)
}

func TestSupply_Get(t *testing.T) {
	c := New()
	cfg := &testConfig{DSN: "sqlite://memory"}
	require.NoError(t, Supply(c, "config", cfg))
	require.NoError(t, c.Start(context.Background()))

	got, err := Get[*testConfig](c, "config")
	require.NoError(t, err)
	assert.Same(t, cfg, got)
}

func TestProvide_DuplicateKey(t *testing.T) {
	c := New()
	require.NoError(t, Supply(c, "x", 1))
	err := Supply(c, "x", 2)
	assert.ErrorIs(t, err, ErrComponentExists)
}

func TestProvide_EmptyKey(t *testing.T) {
	c := New()
	err := Supply(c, "", 1)
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestProvide_NotIdle(t *testing.T) {
	c := New()
	require.NoError(t, Supply(c, "x", 1))
	require.NoError(t, c.Start(context.Background()))
	err := Supply(c, "y", 2)
	assert.ErrorIs(t, err, ErrContainerNotIdle)
}

func TestMustProvide_Panic(t *testing.T) {
	c := New()
	require.NoError(t, Supply(c, "x", 1))
	assert.Panics(t, func() {
		MustSupply(c, "x", 2) // duplicate
	})
}

func TestGet_NotFound(t *testing.T) {
	c := New()
	require.NoError(t, c.Start(context.Background()))
	_, err := Get[int](c, "missing")
	assert.ErrorIs(t, err, ErrComponentNotFound)
}

func TestGet_TypeMismatch(t *testing.T) {
	c := New()
	require.NoError(t, Supply(c, "x", 42))
	require.NoError(t, c.Start(context.Background()))
	_, err := Get[string](c, "x")
	assert.ErrorIs(t, err, ErrTypeMismatch)
}

func TestGet_BeforeStart(t *testing.T) {
	c := New()
	require.NoError(t, Supply(c, "x", 42))
	_, err := Get[int](c, "x")
	assert.ErrorIs(t, err, ErrComponentNotFound)
}

func TestMustGet_Panic(t *testing.T) {
	c := New()
	require.NoError(t, c.Start(context.Background()))
	assert.Panics(t, func() {
		MustGet[int](c, "missing")
	})
}

func TestGet_InterfaceType(t *testing.T) {
	c := New()
	require.NoError(t, Provide(c, "svc", func(_ *Container) (*testService, error) {
		return &testService{healthy: true}, nil
	}))
	require.NoError(t, c.Start(context.Background()))

	// Get as Checker interface
	checker, err := Get[Checker](c, "svc")
	require.NoError(t, err)
	assert.NoError(t, checker.Check(context.Background()))
}

// ---------------------------------------------------------------------------
// Dependency resolution & build order
// ---------------------------------------------------------------------------

func TestStart_DependencyOrder(t *testing.T) {
	c := New()
	var order []string

	Provide(c, "service", func(c *Container) (*orderRecorder, error) {
		_ = MustGet[*orderRecorder](c, "db") // depends on db
		r := &orderRecorder{key: "service", record: &order}
		return r, nil
	})
	Provide(c, "db", func(c *Container) (*orderRecorder, error) {
		_ = MustGet[*orderRecorder](c, "config") // depends on config
		r := &orderRecorder{key: "db", record: &order}
		return r, nil
	})
	Provide(c, "config", func(_ *Container) (*orderRecorder, error) {
		r := &orderRecorder{key: "config", record: &order}
		return r, nil
	})

	require.NoError(t, c.Start(context.Background()))

	// Start order should be: config → db → service (dependency order)
	assert.Equal(t, []string{"start:config", "start:db", "start:service"}, order)

	order = nil
	require.NoError(t, c.Stop(context.Background()))

	// Stop order should be reverse: service → db → config
	assert.Equal(t, []string{"stop:service", "stop:db", "stop:config"}, order)
}

func TestStart_DeepDependencyChain(t *testing.T) {
	c := New()
	var buildOrder []string

	Provide(c, "a", func(c *Container) (string, error) {
		MustGet[string](c, "b")
		buildOrder = append(buildOrder, "a")
		return "a", nil
	})
	Provide(c, "b", func(c *Container) (string, error) {
		MustGet[string](c, "c")
		buildOrder = append(buildOrder, "b")
		return "b", nil
	})
	Provide(c, "c", func(_ *Container) (string, error) {
		buildOrder = append(buildOrder, "c")
		return "c", nil
	})

	require.NoError(t, c.Start(context.Background()))
	assert.Equal(t, []string{"c", "b", "a"}, buildOrder)
}

func TestStart_CircularDependency(t *testing.T) {
	c := New()
	Provide(c, "a", func(c *Container) (int, error) {
		_, err := Get[int](c, "b")
		if err != nil {
			return 0, err
		}
		return 1, nil
	})
	Provide(c, "b", func(c *Container) (int, error) {
		_, err := Get[int](c, "a")
		if err != nil {
			return 0, err
		}
		return 2, nil
	})

	err := c.Start(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCircularDependency)
	assert.Contains(t, err.Error(), "a → b → a")
}

func TestStart_CircularDependency_ThreeWay(t *testing.T) {
	c := New()
	Provide(c, "a", func(c *Container) (int, error) {
		_, err := Get[int](c, "b")
		if err != nil {
			return 0, err
		}
		return 1, nil
	})
	Provide(c, "b", func(c *Container) (int, error) {
		_, err := Get[int](c, "c")
		if err != nil {
			return 0, err
		}
		return 2, nil
	})
	Provide(c, "c", func(c *Container) (int, error) {
		_, err := Get[int](c, "a")
		if err != nil {
			return 0, err
		}
		return 3, nil
	})

	err := c.Start(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCircularDependency)
	assert.Contains(t, err.Error(), "a → b → c → a")
}

func TestStart_ConstructorError(t *testing.T) {
	c := New()
	Provide(c, "bad", func(_ *Container) (int, error) {
		return 0, errors.New("init failed")
	})

	err := c.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "construct bad")
	assert.Contains(t, err.Error(), "init failed")
	assert.Equal(t, StateFailed, c.State())
}

func TestStart_LazyConstruction(t *testing.T) {
	c := New()
	constructed := false
	Provide(c, "x", func(_ *Container) (int, error) {
		constructed = true
		return 42, nil
	})

	// Constructor NOT called at registration time
	assert.False(t, constructed)

	require.NoError(t, c.Start(context.Background()))
	// Constructor called during Start
	assert.True(t, constructed)
}

// ---------------------------------------------------------------------------
// Lifecycle state machine
// ---------------------------------------------------------------------------

func TestStart_StateTransitions(t *testing.T) {
	c := New()
	assert.Equal(t, StateNew, c.State())

	require.NoError(t, Supply(c, "x", 1))
	require.NoError(t, c.Start(context.Background()))
	assert.Equal(t, StateRunning, c.State())

	require.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, StateStopped, c.State())
}

func TestStart_DoubleStart(t *testing.T) {
	c := New()
	require.NoError(t, c.Start(context.Background()))
	err := c.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start")
}

func TestStop_NotRunning(t *testing.T) {
	c := New()
	err := c.Stop(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot stop")
}

// ---------------------------------------------------------------------------
// Start rollback
// ---------------------------------------------------------------------------

func TestStart_Rollback(t *testing.T) {
	c := New()

	good := &testDB{}
	Provide(c, "good", func(_ *Container) (*testDB, error) {
		return good, nil
	})
	Provide(c, "bad", func(_ *Container) (*failStarter, error) {
		return &failStarter{}, nil
	})

	err := c.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start bad")

	// "good" should have been rolled back (stopped)
	assert.True(t, good.stopped)
	assert.Equal(t, StateFailed, c.State())
}

// ---------------------------------------------------------------------------
// Stop
// ---------------------------------------------------------------------------

func TestStop_ErrorAggregation(t *testing.T) {
	c := New()
	Provide(c, "a", func(_ *Container) (*badStopper, error) {
		return &badStopper{name: "a"}, nil
	})
	Provide(c, "b", func(_ *Container) (*badStopper, error) {
		return &badStopper{name: "b"}, nil
	})

	require.NoError(t, c.Start(context.Background()))
	err := c.Stop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop a")
	assert.Contains(t, err.Error(), "stop b")
	assert.Equal(t, StateStopped, c.State())
}

type badStopper struct{ name string }

func (b *badStopper) Stop(context.Context) error {
	return fmt.Errorf("%s stop error", b.name)
}

func TestStop_Timeout(t *testing.T) {
	c := New(WithStopTimeout(50 * time.Millisecond))
	Provide(c, "slow", func(_ *Container) (*slowStopper, error) {
		return &slowStopper{}, nil
	})
	require.NoError(t, c.Start(context.Background()))

	err := c.Stop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

type slowStopper struct{}

func (s *slowStopper) Stop(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return nil
	}
}

// ---------------------------------------------------------------------------
// Restart
// ---------------------------------------------------------------------------

func TestRestart(t *testing.T) {
	c := New()
	callCount := 0
	Provide(c, "x", func(_ *Container) (int, error) {
		callCount++
		return callCount, nil
	})

	require.NoError(t, c.Start(context.Background()))
	v1 := MustGet[int](c, "x")
	assert.Equal(t, 1, v1)

	require.NoError(t, c.Restart(context.Background()))
	v2 := MustGet[int](c, "x")
	assert.Equal(t, 2, v2) // constructor called again
	assert.Equal(t, StateRunning, c.State())
}

func TestProvide_AfterStop(t *testing.T) {
	c := New()
	require.NoError(t, Supply(c, "x", 1))
	require.NoError(t, c.Start(context.Background()))
	require.NoError(t, c.Stop(context.Background()))

	// Should be able to register new components after Stop
	require.NoError(t, Supply(c, "y", 2))
	require.NoError(t, c.Start(context.Background()))

	v, err := Get[int](c, "y")
	require.NoError(t, err)
	assert.Equal(t, 2, v)
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

func TestHealthCheck(t *testing.T) {
	c := New()
	Provide(c, "healthy", func(_ *Container) (*testService, error) {
		return &testService{healthy: true}, nil
	})
	Provide(c, "unhealthy", func(_ *Container) (*testService, error) {
		return &testService{healthy: false}, nil
	})
	Provide(c, "no-checker", func(_ *Container) (int, error) {
		return 42, nil
	})

	require.NoError(t, c.Start(context.Background()))
	report := c.HealthCheck(context.Background())

	assert.False(t, report.Healthy)
	assert.Len(t, report.Components, 3)

	// Find specific results
	for _, ch := range report.Components {
		switch ch.Key {
		case "healthy":
			assert.True(t, ch.Healthy)
			assert.NoError(t, ch.Error)
		case "unhealthy":
			assert.False(t, ch.Healthy)
			assert.Error(t, ch.Error)
		case "no-checker":
			assert.True(t, ch.Healthy) // no Checker interface → healthy
		}
	}
}

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

func TestHooks_Order(t *testing.T) {
	var order []string
	c := New(
		WithOnStart(func(context.Context) error {
			order = append(order, "onStart")
			return nil
		}),
		WithOnStarted(func(context.Context) error {
			order = append(order, "onStarted")
			return nil
		}),
		WithOnStopping(func(context.Context) error {
			order = append(order, "onStopping")
			return nil
		}),
		WithOnStop(func(context.Context) error {
			order = append(order, "onStop")
			return nil
		}),
	)

	Provide(c, "svc", func(_ *Container) (*orderRecorder, error) {
		return &orderRecorder{key: "svc", record: &order}, nil
	})

	require.NoError(t, c.Start(context.Background()))
	require.NoError(t, c.Stop(context.Background()))

	assert.Equal(t, []string{
		"onStart",
		"start:svc",
		"onStarted",
		"onStopping",
		"stop:svc",
		"onStop",
	}, order)
}

func TestHooks_OnStartError(t *testing.T) {
	c := New(WithOnStart(func(context.Context) error {
		return errors.New("hook failed")
	}))
	Supply(c, "x", 1)

	err := c.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "onStart hook")
	assert.Equal(t, StateFailed, c.State())
}

func TestHooks_OnStartedError_Rollback(t *testing.T) {
	c := New(WithOnStarted(func(context.Context) error {
		return errors.New("hook failed")
	}))

	svc := &testDB{}
	Provide(c, "svc", func(_ *Container) (*testDB, error) { return svc, nil })

	err := c.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "onStarted hook")
	assert.True(t, svc.stopped) // rollback
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func TestKeys_Has_Count(t *testing.T) {
	c := New()
	Supply(c, "a", 1)
	Supply(c, "b", 2)
	Supply(c, "c", 3)

	assert.Equal(t, []string{"a", "b", "c"}, c.Keys())
	assert.True(t, c.Has("a"))
	assert.False(t, c.Has("z"))
	assert.Equal(t, 3, c.Count())
}

func TestMetrics(t *testing.T) {
	c := New()
	Supply(c, "x", 1)
	Supply(c, "y", 2)

	m := c.Metrics()
	assert.Equal(t, 2, m.ComponentCount)
	assert.Equal(t, StateNew, m.State)
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestConcurrentGet(t *testing.T) {
	c := New()
	Supply(c, "x", 42)
	require.NoError(t, c.Start(context.Background()))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := Get[int](c, "x")
			assert.NoError(t, err)
			assert.Equal(t, 42, v)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Global container
// ---------------------------------------------------------------------------

func TestGlobalC(t *testing.T) {
	assert.NotNil(t, C)
	assert.Equal(t, StateNew, C.State())
}
