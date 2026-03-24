package dig

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

// State represents the lifecycle phase of a Container.
type State int

const (
	StateNew      State = iota // Container created, not yet started
	StateStarting              // Start() in progress
	StateRunning               // All components started
	StateStopping              // Stop() in progress
	StateStopped               // All components stopped
	StateError                 // An unrecoverable error occurred
)

func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// HealthReport / ContainerMetrics
// ---------------------------------------------------------------------------

// ComponentHealth holds the health check result for a single component.
type ComponentHealth struct {
	Name    string
	Healthy bool
	Error   error
}

// HealthReport aggregates health check results for all components.
type HealthReport struct {
	Components []ComponentHealth
	Healthy    bool
}

// ContainerMetrics holds basic counts about the container.
type ContainerMetrics struct {
	GroupCount     int
	ComponentCount int
	State          State
}

// ---------------------------------------------------------------------------
// Option
// ---------------------------------------------------------------------------

// Option configures a Container.
type Option func(*Container)

// WithStopTimeout sets the timeout used when stopping individual components.
func WithStopTimeout(d time.Duration) Option {
	return func(c *Container) { c.stopTimeout = d }
}

// WithGroup pre-registers a group with the given name and priority order.
// Lower order values are started first and stopped last.
func WithGroup(name string, order int) Option {
	return func(c *Container) {
		if _, exists := c.groups[name]; !exists {
			c.groups[name] = newGroup(name, order)
		}
	}
}

// WithOnStart registers a hook that runs before the first component is started.
func WithOnStart(fn func(ctx context.Context) error) Option {
	return func(c *Container) { c.onStart = append(c.onStart, fn) }
}

// WithOnStarted registers a hook that runs after all components have started.
func WithOnStarted(fn func(ctx context.Context) error) Option {
	return func(c *Container) { c.onStarted = append(c.onStarted, fn) }
}

// WithOnStopping registers a hook that runs before the first component is stopped.
func WithOnStopping(fn func(ctx context.Context) error) Option {
	return func(c *Container) { c.onStopping = append(c.onStopping, fn) }
}

// WithOnStop registers a hook that runs after all components have stopped.
func WithOnStop(fn func(ctx context.Context) error) Option {
	return func(c *Container) { c.onStop = append(c.onStop, fn) }
}

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

// C is the package-level default Container. It is ready to use immediately
// and can be replaced by calling New with the result stored in C.
var C *Container

func init() { C = New() }

// Container is a lightweight dependency-injection container that manages
// component registration, dependency resolution, and a Start/Stop lifecycle.
//
// Components are organised into named groups. Within each group they are
// started in ascending Order and stopped in reverse order. Groups themselves
// are started in ascending order and stopped in reverse order.
type Container struct {
	mu     sync.RWMutex
	groups map[string]*group
	state  State

	stopTimeout time.Duration

	// Lifecycle hooks — each slice is called in registration order.
	onStart    []func(ctx context.Context) error
	onStarted  []func(ctx context.Context) error
	onStopping []func(ctx context.Context) error
	onStop     []func(ctx context.Context) error
}

// New creates an empty Container pre-populated with the five default groups.
func New(opts ...Option) *Container {
	c := &Container{
		groups:      make(map[string]*group),
		stopTimeout: 30 * time.Second,
	}
	for name, order := range defaultGroupOrders {
		c.groups[name] = newGroup(name, order)
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// Provide registers comp in the named group. If order values are given the
// first one is used; otherwise 0 is assumed.
// Returns ErrGroupNotFound if the group has not been created, ErrAlreadyExists
// if a component with the same name is already registered.
func (c *Container) Provide(groupName string, comp Component, order ...int) error {
	if comp == nil || comp.Name() == "" {
		return fmt.Errorf("%w: component is nil or has empty name", ErrInvalidComponent)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	g, ok := c.groups[groupName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrGroupNotFound, groupName)
	}
	if _, exists := g.components[comp.Name()]; exists {
		return fmt.Errorf("%w: %s/%s", ErrComponentAlreadyExists, groupName, comp.Name())
	}

	o := 0
	if len(order) > 0 {
		o = order[0]
	} else if ord, ok := comp.(Orderable); ok {
		o = ord.Order()
	}

	d := &Descriptor{
		Name:     comp.Name(),
		Instance: comp,
		Order:    o,
		Group:    groupName,
	}
	g.add(d)
	return nil
}

// MustProvide is like Provide but panics on error.
func (c *Container) MustProvide(groupName string, comp Component, order ...int) {
	if err := c.Provide(groupName, comp, order...); err != nil {
		panic(fmt.Sprintf("dig: MustProvide: %v", err))
	}
}

// ProvideGroup creates a new group with the given name and priority order.
// Returns ErrGroupAlreadyExists if the group already exists.
func (c *Container) ProvideGroup(name string, order int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.groups[name]; exists {
		return fmt.Errorf("%w: %s", ErrGroupAlreadyExists, name)
	}
	c.groups[name] = newGroup(name, order)
	return nil
}

// Convenience registration helpers for the five default groups.

func (c *Container) ProvideConfig(comp Component, order ...int) error {
	return c.Provide(ConfigGroup, comp, order...)
}

func (c *Container) MustProvideConfig(comp Component, order ...int) {
	c.MustProvide(ConfigGroup, comp, order...)
}

func (c *Container) ProvideDatabase(comp Component, order ...int) error {
	return c.Provide(DatabaseGroup, comp, order...)
}

func (c *Container) MustProvideDatabase(comp Component, order ...int) {
	c.MustProvide(DatabaseGroup, comp, order...)
}

func (c *Container) ProvideService(comp Component, order ...int) error {
	return c.Provide(ServiceGroup, comp, order...)
}

func (c *Container) MustProvideService(comp Component, order ...int) {
	c.MustProvide(ServiceGroup, comp, order...)
}

func (c *Container) ProvideHandler(comp Component, order ...int) error {
	return c.Provide(HandlerGroup, comp, order...)
}

func (c *Container) MustProvideHandler(comp Component, order ...int) {
	c.MustProvide(HandlerGroup, comp, order...)
}

func (c *Container) ProvideController(comp Component, order ...int) error {
	return c.Provide(ControllerGroup, comp, order...)
}

func (c *Container) MustProvideController(comp Component, order ...int) {
	c.MustProvide(ControllerGroup, comp, order...)
}

// ---------------------------------------------------------------------------
// Retrieval
// ---------------------------------------------------------------------------

// Get returns the component registered under group/name.
func (c *Container) Get(groupName, name string) (Component, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.groups[groupName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrGroupNotFound, groupName)
	}
	d, ok := g.components[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", ErrComponentNotFound, groupName, name)
	}
	return d.Instance, nil
}

// MustGet is like Get but panics on error.
func (c *Container) MustGet(groupName, name string) Component {
	comp, err := c.Get(groupName, name)
	if err != nil {
		panic(fmt.Sprintf("dig: MustGet: %v", err))
	}
	return comp
}

// GetAll returns all components in the named group, sorted by Order.
func (c *Container) GetAll(groupName string) ([]Component, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.groups[groupName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrGroupNotFound, groupName)
	}
	descs := g.getSorted()
	comps := make([]Component, len(descs))
	for i, d := range descs {
		comps[i] = d.Instance
	}
	return comps, nil
}

// Invoke returns the component at group/name cast to type T.
// Returns an error when the lookup fails or the type assertion fails.
func Invoke[T any](c *Container, groupName, name string) (T, error) {
	var zero T
	comp, err := c.Get(groupName, name)
	if err != nil {
		return zero, err
	}
	t, ok := comp.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s/%s cannot be cast to requested type", ErrInvalidComponent, groupName, name)
	}
	return t, nil
}

// MustInvoke is like Invoke but panics on error.
func MustInvoke[T any](c *Container, groupName, name string) T {
	t, err := Invoke[T](c, groupName, name)
	if err != nil {
		panic(fmt.Sprintf("dig: MustInvoke: %v", err))
	}
	return t
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

// Groups returns the names of all registered groups.
func (c *Container) Groups() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.groups))
	for name := range c.groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HasGroup reports whether a group with the given name is registered.
func (c *Container) HasGroup(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.groups[name]
	return ok
}

// Has reports whether a component name/name exists in the given group.
func (c *Container) Has(groupName, name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.groups[groupName]
	if !ok {
		return false
	}
	_, ok = g.components[name]
	return ok
}

// Count returns the total number of registered components across all groups.
func (c *Container) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := 0
	for _, g := range c.groups {
		total += len(g.components)
	}
	return total
}

// State returns the current lifecycle state.
func (c *Container) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start resolves dependencies, then starts all components in group/order.
// Hooks: onStart → components → onStarted.
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.state != StateNew && c.state != StateStopped {
		c.mu.Unlock()
		return fmt.Errorf("dig: cannot start container in state %s", c.state)
	}
	c.state = StateStarting
	c.mu.Unlock()

	inj := newInjector(c)
	if err := inj.Resolve(ctx); err != nil {
		c.mu.Lock()
		c.state = StateError
		c.mu.Unlock()
		return fmt.Errorf("dig: dependency resolution failed: %w", err)
	}

	// onStart hooks
	for _, fn := range c.onStart {
		if err := fn(ctx); err != nil {
			c.mu.Lock()
			c.state = StateError
			c.mu.Unlock()
			return fmt.Errorf("dig: onStart hook: %w", err)
		}
	}

	// Start components in group order, then component order within each group.
	groups := c.sortedGroups()
	for _, g := range groups {
		descs := func() []*Descriptor {
			c.mu.RLock()
			defer c.mu.RUnlock()
			return g.getSorted()
		}()
		for _, d := range descs {
			if s, ok := d.Instance.(Starter); ok {
				if err := s.Start(ctx); err != nil {
					c.mu.Lock()
					c.state = StateError
					c.mu.Unlock()
					return fmt.Errorf("dig: start %s/%s: %w", g.name, d.Name, err)
				}
			}
		}
	}

	// onStarted hooks — called exactly once, here.
	for _, fn := range c.onStarted {
		if err := fn(ctx); err != nil {
			c.mu.Lock()
			c.state = StateError
			c.mu.Unlock()
			return fmt.Errorf("dig: onStarted hook: %w", err)
		}
	}

	c.mu.Lock()
	c.state = StateRunning
	c.mu.Unlock()
	return nil
}

// Stop stops all components in reverse group/order.
// Hooks: onStopping → components (reverse) → onStop.
func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.state != StateRunning && c.state != StateError {
		c.mu.Unlock()
		return fmt.Errorf("dig: cannot stop container in state %s", c.state)
	}
	c.state = StateStopping
	c.mu.Unlock()

	// onStopping hooks
	for _, fn := range c.onStopping {
		if err := fn(ctx); err != nil {
			// Log but do not abort — we must attempt to stop all components.
			_ = err
		}
	}

	// Stop in reverse group order; within each group reverse component order.
	groups := c.sortedGroups()
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		descs := func() []*Descriptor {
			c.mu.RLock()
			defer c.mu.RUnlock()
			return g.getSorted()
		}()
		for j := len(descs) - 1; j >= 0; j-- {
			d := descs[j]
			if s, ok := d.Instance.(Stopper); ok {
				stopCtx, cancel := context.WithTimeout(ctx, c.stopTimeout)
				err := s.Stop(stopCtx)
				cancel()
				if err != nil {
					// Log but continue stopping remaining components.
					_ = err
				}
			}
		}
	}

	// onStop hooks
	for _, fn := range c.onStop {
		_ = fn(ctx)
	}

	c.mu.Lock()
	c.state = StateStopped
	c.mu.Unlock()
	return nil
}

// Restart stops then starts the container.
func (c *Container) Restart(ctx context.Context) error {
	if err := c.Stop(ctx); err != nil {
		return err
	}
	return c.Start(ctx)
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

// HealthCheck runs the Check method of every component that implements Checker
// and returns an aggregated report.
func (c *Container) HealthCheck(ctx context.Context) *HealthReport {
	groups := c.sortedGroups()
	report := &HealthReport{Healthy: true}

	for _, g := range groups {
		descs := func() []*Descriptor {
			c.mu.RLock()
			defer c.mu.RUnlock()
			return g.getSorted()
		}()
		for _, d := range descs {
			ch := ComponentHealth{Name: d.Name, Healthy: true}
			if checker, ok := d.Instance.(Checker); ok {
				if err := checker.Check(ctx); err != nil {
					ch.Healthy = false
					ch.Error = err
					report.Healthy = false
				}
			}
			report.Components = append(report.Components, ch)
		}
	}
	return report
}

// Metrics returns basic container metrics.
func (c *Container) Metrics() *ContainerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := 0
	for _, g := range c.groups {
		total += len(g.components)
	}
	return &ContainerMetrics{
		GroupCount:     len(c.groups),
		ComponentCount: total,
		State:          c.state,
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// sortedGroups returns groups sorted by their order field (ascending).
func (c *Container) sortedGroups() []*group {
	c.mu.RLock()
	defer c.mu.RUnlock()
	groups := make([]*group, 0, len(c.groups))
	for _, g := range c.groups {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].order < groups[j].order
	})
	return groups
}
