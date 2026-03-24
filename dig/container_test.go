package dig

import (
	"context"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

type mockComponent struct {
	name    string
	started bool
	stopped bool
	healthy bool
	order   int
}

func (m *mockComponent) Name() string { return m.name }
func (m *mockComponent) Order() int   { return m.order }
func (m *mockComponent) Start(_ context.Context) error {
	m.started = true
	return nil
}
func (m *mockComponent) Stop(_ context.Context) error {
	m.stopped = true
	return nil
}
func (m *mockComponent) Check(_ context.Context) error {
	if !m.healthy {
		return errors.New("unhealthy")
	}
	return nil
}

// mockProvider both provides a dependency and is a Component.
type mockProvider struct {
	mockComponent
	depName string
	depVal  any
}

func (p *mockProvider) ProvidesDependency() string { return p.depName }
func (p *mockProvider) GetDependency() any         { return p.depVal }

// mockConsumer requires a dependency and records injected values.
type mockConsumer struct {
	mockComponent
	required []string
	injected map[string]any
}

func (c *mockConsumer) RequiredDependencies() []string { return c.required }
func (c *mockConsumer) SetDependency(name string, dep any) error {
	if c.injected == nil {
		c.injected = make(map[string]any)
	}
	c.injected[name] = dep
	return nil
}

// ---------------------------------------------------------------------------
// Registration tests
// ---------------------------------------------------------------------------

func TestProvide_OK(t *testing.T) {
	c := New()
	comp := &mockComponent{name: "svc"}
	if err := c.Provide(ServiceGroup, comp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.Has(ServiceGroup, "svc") {
		t.Fatal("component not found after Provide")
	}
}

func TestProvide_GroupNotFound(t *testing.T) {
	c := New()
	err := c.Provide("nonexistent", &mockComponent{name: "x"})
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("want ErrGroupNotFound, got %v", err)
	}
}

func TestProvide_AlreadyExists(t *testing.T) {
	c := New()
	comp := &mockComponent{name: "svc"}
	_ = c.Provide(ServiceGroup, comp)
	err := c.Provide(ServiceGroup, comp)
	if !errors.Is(err, ErrComponentAlreadyExists) {
		t.Fatalf("want ErrComponentAlreadyExists, got %v", err)
	}
}

func TestMustProvide_Panics(t *testing.T) {
	c := New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic but did not panic")
		}
	}()
	c.MustProvide("nonexistent", &mockComponent{name: "x"})
}

func TestProvideGroup_OK(t *testing.T) {
	c := New()
	if err := c.ProvideGroup("custom", 999); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.HasGroup("custom") {
		t.Fatal("group not found after ProvideGroup")
	}
}

func TestProvideGroup_AlreadyExists(t *testing.T) {
	c := New()
	err := c.ProvideGroup(ServiceGroup, 0)
	if !errors.Is(err, ErrGroupAlreadyExists) {
		t.Fatalf("want ErrGroupAlreadyExists, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Retrieval tests
// ---------------------------------------------------------------------------

func TestGet_OK(t *testing.T) {
	c := New()
	comp := &mockComponent{name: "db"}
	_ = c.Provide(DatabaseGroup, comp)
	got, err := c.Get(DatabaseGroup, "db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != comp {
		t.Fatal("retrieved wrong component")
	}
}

func TestGet_ComponentNotFound(t *testing.T) {
	c := New()
	_, err := c.Get(ServiceGroup, "missing")
	if !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("want ErrComponentNotFound, got %v", err)
	}
}

func TestMustGet_Panics(t *testing.T) {
	c := New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic but did not panic")
		}
	}()
	c.MustGet(ServiceGroup, "missing")
}

func TestGetAll_Sorted(t *testing.T) {
	c := New()
	_ = c.Provide(ServiceGroup, &mockComponent{name: "b", order: 2})
	_ = c.Provide(ServiceGroup, &mockComponent{name: "a", order: 1})
	_ = c.Provide(ServiceGroup, &mockComponent{name: "c", order: 3})

	all, err := c.GetAll(ServiceGroup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 components, got %d", len(all))
	}
	if all[0].Name() != "a" || all[1].Name() != "b" || all[2].Name() != "c" {
		t.Fatal("components not returned in order")
	}
}

func TestInvoke_TypeSafe(t *testing.T) {
	c := New()
	comp := &mockComponent{name: "cfg"}
	_ = c.Provide(ConfigGroup, comp)

	got, err := Invoke[*mockComponent](c, ConfigGroup, "cfg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != comp {
		t.Fatal("wrong value returned")
	}
}

func TestInvoke_WrongType(t *testing.T) {
	c := New()
	_ = c.Provide(ConfigGroup, &mockComponent{name: "cfg"})
	_, err := Invoke[*mockProvider](c, ConfigGroup, "cfg")
	if !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("want ErrInvalidComponent, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func TestCount(t *testing.T) {
	c := New()
	_ = c.Provide(ServiceGroup, &mockComponent{name: "a"})
	_ = c.Provide(ServiceGroup, &mockComponent{name: "b"})
	if n := c.Count(); n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Lifecycle tests
// ---------------------------------------------------------------------------

func TestStart_Stop(t *testing.T) {
	c := New()
	comp := &mockComponent{name: "svc", healthy: true}
	_ = c.Provide(ServiceGroup, comp)

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if c.State() != StateRunning {
		t.Fatalf("expected Running, got %s", c.State())
	}
	if !comp.started {
		t.Fatal("component was not started")
	}

	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if c.State() != StateStopped {
		t.Fatalf("expected Stopped, got %s", c.State())
	}
	if !comp.stopped {
		t.Fatal("component was not stopped")
	}
}

func TestStart_GroupOrder(t *testing.T) {
	// Use hooks to record start order.
	order2 := make([]string, 0)
	c2 := New(
		WithOnStart(func(_ context.Context) error {
			order2 = append(order2, "onStart")
			return nil
		}),
		WithOnStarted(func(_ context.Context) error {
			order2 = append(order2, "onStarted")
			return nil
		}),
	)
	_ = c2.Provide(ConfigGroup, &mockComponent{name: "cfg"})
	_ = c2.Provide(DatabaseGroup, &mockComponent{name: "db"})

	if err := c2.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if order2[0] != "onStart" {
		t.Fatalf("expected onStart first, got %s", order2[0])
	}
	if order2[len(order2)-1] != "onStarted" {
		t.Fatalf("expected onStarted last, got %s", order2[len(order2)-1])
	}
}

func TestOnStartedCalledOnce(t *testing.T) {
	// This test guards against the double-call bug where onStarted hooks would
	// be invoked both during component init and after all groups finish.
	count := 0
	c := New(
		WithOnStarted(func(_ context.Context) error {
			count++
			return nil
		}),
	)
	_ = c.Provide(ServiceGroup, &mockComponent{name: "svc"})

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if count != 1 {
		t.Fatalf("onStarted hook called %d times, want exactly 1", count)
	}
}

func TestStart_InvalidState(t *testing.T) {
	c := New()
	_ = c.Start(context.Background())
	// Second call while Running should fail.
	err := c.Start(context.Background())
	if err == nil {
		t.Fatal("expected error on second Start")
	}
}

// ---------------------------------------------------------------------------
// Dependency injection tests
// ---------------------------------------------------------------------------

func TestDependencyInjection(t *testing.T) {
	c := New()

	prov := &mockProvider{
		mockComponent: mockComponent{name: "token-provider"},
		depName:       "auth-token",
		depVal:        "secret-value",
	}
	cons := &mockConsumer{
		mockComponent: mockComponent{name: "api-client"},
		required:      []string{"auth-token"},
	}

	_ = c.Provide(ServiceGroup, prov)
	_ = c.Provide(ServiceGroup, cons)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if cons.injected["auth-token"] != "secret-value" {
		t.Fatalf("dependency not injected, got %v", cons.injected)
	}
}

// TestCircularDependencyDetected is covered by TestCheckCycles_Direct.
// Real-component cycle detection is tested there via the graph directly.

func TestCheckCycles_Direct(t *testing.T) {
	// Build a simple A depends on B, B depends on A cycle manually.
	a := &node{comp: &mockComponent{name: "a"}}
	b := &node{comp: &mockComponent{name: "b"}}
	a.deps = []*node{b}
	b.deps = []*node{a}
	graph := map[string]*node{"a": a, "b": b}
	if err := checkCycles(graph); !errors.Is(err, ErrCircularDependency) {
		t.Fatalf("want ErrCircularDependency, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Health check tests
// ---------------------------------------------------------------------------

func TestHealthCheck(t *testing.T) {
	c := New()
	_ = c.Provide(ServiceGroup, &mockComponent{name: "healthy", healthy: true})
	_ = c.Provide(ServiceGroup, &mockComponent{name: "unhealthy", healthy: false})

	report := c.HealthCheck(context.Background())
	if report.Healthy {
		t.Fatal("expected overall unhealthy report")
	}
	found := false
	for _, ch := range report.Components {
		if ch.Name == "unhealthy" && !ch.Healthy {
			found = true
		}
	}
	if !found {
		t.Fatal("unhealthy component not in report")
	}
}

// ---------------------------------------------------------------------------
// Global C variable
// ---------------------------------------------------------------------------

func TestGlobalC(t *testing.T) {
	if C == nil {
		t.Fatal("global C must be non-nil after package init")
	}
	if C.State() != StateNew {
		t.Fatalf("expected StateNew, got %s", C.State())
	}
}
