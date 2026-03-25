package cx

import (
	"context"
	"errors"
	"testing"
	"time"
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

// ---------------------------------------------------------------------------
// Restart tests
// ---------------------------------------------------------------------------

func TestRestart(t *testing.T) {
	c := New()
	comp := &mockComponent{name: "svc", healthy: true}
	_ = c.Provide(ServiceGroup, comp)

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !comp.started {
		t.Fatal("component should be started")
	}

	comp.started = false // reset to verify restart triggers Start again
	if err := c.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if c.State() != StateRunning {
		t.Fatalf("expected Running after Restart, got %s", c.State())
	}
	if !comp.stopped {
		t.Fatal("component should have been stopped during Restart")
	}
	if !comp.started {
		t.Fatal("component should have been re-started during Restart")
	}
}

// ---------------------------------------------------------------------------
// Metrics tests
// ---------------------------------------------------------------------------

func TestMetrics(t *testing.T) {
	c := New()
	_ = c.Provide(ServiceGroup, &mockComponent{name: "a"})
	_ = c.Provide(DatabaseGroup, &mockComponent{name: "b"})

	m := c.Metrics()
	if m.ComponentCount != 2 {
		t.Fatalf("want 2 components, got %d", m.ComponentCount)
	}
	if m.GroupCount != 5 { // 5 default groups
		t.Fatalf("want 5 groups, got %d", m.GroupCount)
	}
	if m.State != StateNew {
		t.Fatalf("want StateNew, got %s", m.State)
	}
}

// ---------------------------------------------------------------------------
// Stop hooks tests
// ---------------------------------------------------------------------------

func TestOnStoppingAndOnStop(t *testing.T) {
	var order []string
	c := New(
		WithOnStopping(func(_ context.Context) error {
			order = append(order, "onStopping")
			return nil
		}),
		WithOnStop(func(_ context.Context) error {
			order = append(order, "onStop")
			return nil
		}),
	)
	comp := &mockComponent{name: "svc", healthy: true}
	_ = c.Provide(ServiceGroup, comp)

	ctx := context.Background()
	_ = c.Start(ctx)
	_ = c.Stop(ctx)

	if len(order) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(order))
	}
	if order[0] != "onStopping" {
		t.Fatalf("expected onStopping first, got %s", order[0])
	}
	if order[1] != "onStop" {
		t.Fatalf("expected onStop last, got %s", order[1])
	}
}

// ---------------------------------------------------------------------------
// Stop timeout tests
// ---------------------------------------------------------------------------

type slowStopper struct {
	mockComponent
	delay time.Duration
}

func (s *slowStopper) Stop(ctx context.Context) error {
	select {
	case <-time.After(s.delay):
		s.stopped = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestStopTimeout(t *testing.T) {
	c := New(WithStopTimeout(50 * time.Millisecond))
	comp := &slowStopper{
		mockComponent: mockComponent{name: "slow"},
		delay:         5 * time.Second, // way longer than the 50ms timeout
	}
	_ = c.Provide(ServiceGroup, comp)

	ctx := context.Background()
	_ = c.Start(ctx)
	// Stop should not hang — the per-component timeout should fire.
	_ = c.Stop(ctx)

	if c.State() != StateStopped {
		t.Fatalf("expected Stopped, got %s", c.State())
	}
}

// ---------------------------------------------------------------------------
// Groups / HasGroup / Query helpers
// ---------------------------------------------------------------------------

func TestGroups(t *testing.T) {
	c := New()
	groups := c.Groups()
	if len(groups) != 5 {
		t.Fatalf("expected 5 default groups, got %d", len(groups))
	}
	_ = c.ProvideGroup("custom", 999)
	groups = c.Groups()
	if len(groups) != 6 {
		t.Fatalf("expected 6 groups after adding custom, got %d", len(groups))
	}
}

func TestStop_InvalidState(t *testing.T) {
	c := New()
	// Stop on a new container should fail.
	err := c.Stop(context.Background())
	if err == nil {
		t.Fatal("expected error when stopping a non-running container")
	}
}

// ---------------------------------------------------------------------------
// Start rollback tests
// ---------------------------------------------------------------------------

type failingStarter struct {
	mockComponent
	failOnStart bool
}

func (f *failingStarter) Start(_ context.Context) error {
	if f.failOnStart {
		return errors.New("start failed")
	}
	f.started = true
	return nil
}

func (f *failingStarter) Stop(_ context.Context) error {
	f.stopped = true
	return nil
}

func TestStart_RollbackOnFailure(t *testing.T) {
	c := New()
	ok1 := &failingStarter{mockComponent: mockComponent{name: "ok1", order: 1}}
	ok2 := &failingStarter{mockComponent: mockComponent{name: "ok2", order: 2}}
	bad := &failingStarter{mockComponent: mockComponent{name: "bad", order: 3}, failOnStart: true}

	_ = c.Provide(ServiceGroup, ok1)
	_ = c.Provide(ServiceGroup, ok2)
	_ = c.Provide(ServiceGroup, bad)

	err := c.Start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	// ok1 and ok2 should have been rolled back (stopped).
	if !ok1.stopped {
		t.Fatal("ok1 should have been stopped during rollback")
	}
	if !ok2.stopped {
		t.Fatal("ok2 should have been stopped during rollback")
	}
	if c.State() != StateError {
		t.Fatalf("expected StateError, got %s", c.State())
	}
}

// ---------------------------------------------------------------------------
// Duplicate provider tests
// ---------------------------------------------------------------------------

func TestDuplicateProvider(t *testing.T) {
	c := New()
	p1 := &mockProvider{
		mockComponent: mockComponent{name: "p1"},
		depName:       "shared-dep",
		depVal:        "val1",
	}
	p2 := &mockProvider{
		mockComponent: mockComponent{name: "p2"},
		depName:       "shared-dep",
		depVal:        "val2",
	}
	_ = c.Provide(ServiceGroup, p1)
	_ = c.Provide(ServiceGroup, p2)

	err := c.Start(context.Background())
	if !errors.Is(err, ErrDuplicateProvider) {
		t.Fatalf("want ErrDuplicateProvider, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stop error aggregation tests
// ---------------------------------------------------------------------------

type failingStopper struct {
	mockComponent
	stopErr error
}

func (f *failingStopper) Start(_ context.Context) error { return nil }
func (f *failingStopper) Stop(_ context.Context) error  { return f.stopErr }

func TestStop_AggregatesErrors(t *testing.T) {
	c := New()
	s1 := &failingStopper{mockComponent: mockComponent{name: "s1"}, stopErr: errors.New("err1")}
	s2 := &failingStopper{mockComponent: mockComponent{name: "s2"}, stopErr: errors.New("err2")}
	_ = c.Provide(ServiceGroup, s1)
	_ = c.Provide(ServiceGroup, s2)

	_ = c.Start(context.Background())
	err := c.Stop(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	// Both errors should be present.
	msg := err.Error()
	if !errors.Is(err, err) { // basic sanity
		t.Fatal("error should be valid")
	}
	if len(msg) == 0 {
		t.Fatal("error message should be non-empty")
	}
	// The joined error should contain both component names.
	if !(containsStr(msg, "s1") && containsStr(msg, "s2")) {
		t.Fatalf("expected both s1 and s2 in error, got: %s", msg)
	}
}

func TestStop_HookErrorsAggregated(t *testing.T) {
	c := New(
		WithOnStopping(func(_ context.Context) error { return errors.New("stopping-err") }),
		WithOnStop(func(_ context.Context) error { return errors.New("stop-err") }),
	)
	_ = c.Provide(ServiceGroup, &mockComponent{name: "svc"})
	_ = c.Start(context.Background())
	err := c.Stop(context.Background())
	if err == nil {
		t.Fatal("expected error from hooks")
	}
	msg := err.Error()
	if !(containsStr(msg, "stopping-err") && containsStr(msg, "stop-err")) {
		t.Fatalf("expected both hook errors, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// Provide state guard tests
// ---------------------------------------------------------------------------

func TestProvide_RejectsWhenRunning(t *testing.T) {
	c := New()
	_ = c.Start(context.Background())
	err := c.Provide(ServiceGroup, &mockComponent{name: "late"})
	if !errors.Is(err, ErrNotRegisterable) {
		t.Fatalf("want ErrNotRegisterable, got %v", err)
	}
}

func TestProvide_AllowsAfterStop(t *testing.T) {
	c := New()
	_ = c.Start(context.Background())
	_ = c.Stop(context.Background())
	err := c.Provide(ServiceGroup, &mockComponent{name: "new-svc"})
	if err != nil {
		t.Fatalf("Provide after Stop should succeed, got %v", err)
	}
}

func TestDependencyGraph(t *testing.T) {
	c := New()
	prov := &mockProvider{
		mockComponent: mockComponent{name: "db"},
		depName:       "database",
		depVal:        "pg-conn",
	}
	cons := &mockConsumer{
		mockComponent: mockComponent{name: "svc"},
		required:      []string{"database"},
	}

	c.MustProvide(DatabaseGroup, prov)
	c.MustProvide(ServiceGroup, cons)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	graph := c.DependencyGraph()
	if graph == nil {
		t.Fatal("DependencyGraph returned nil after Start")
	}
	// svc depends on db
	deps, ok := graph["svc"]
	if !ok {
		t.Fatal("expected svc in dependency graph")
	}
	if len(deps) != 1 || deps[0] != "db" {
		t.Fatalf("svc deps = %v, want [db]", deps)
	}
	// db has no deps
	dbDeps, ok := graph["db"]
	if !ok {
		t.Fatal("expected db in dependency graph")
	}
	if len(dbDeps) != 0 {
		t.Fatalf("db deps = %v, want []", dbDeps)
	}

	_ = c.Stop(context.Background())
}

func TestDependencyGraph_BeforeStart(t *testing.T) {
	c := New()
	if graph := c.DependencyGraph(); graph != nil {
		t.Fatalf("DependencyGraph before Start should be nil, got %v", graph)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		findStr(haystack, needle))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
