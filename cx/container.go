package cx

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	StateFailed                // Start or Stop failed
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
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Health / Metrics
// ---------------------------------------------------------------------------

// ComponentHealth holds the health-check result for a single component.
type ComponentHealth struct {
	Key     string
	Healthy bool
	Error   error
}

// HealthReport aggregates health-check results.
type HealthReport struct {
	Components []ComponentHealth
	Healthy    bool
}

// ContainerMetrics holds basic counts about the container.
type ContainerMetrics struct {
	ComponentCount int
	State          State
}

// ---------------------------------------------------------------------------
// Option
// ---------------------------------------------------------------------------

// Option configures a Container.
type Option func(*Container)

// WithStopTimeout sets the per-component timeout used during Stop.
func WithStopTimeout(d time.Duration) Option {
	return func(c *Container) { c.stopTimeout = d }
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
// provider (internal)
// ---------------------------------------------------------------------------

type provider struct {
	key         string
	constructor func(*Container) (any, error)
	value       any
	built       bool
}

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

// C is the package-level default Container, ready to use immediately.
var C *Container

func init() { C = New() }

// Container is a lightweight dependency-injection container that manages
// component registration, lazy construction, and a Start/Stop lifecycle.
//
// Components are registered as constructors via [Provide] or as ready-made
// values via [Supply]. During [Container.Start] every constructor is invoked
// (lazily – dependencies are built on first access). Values that implement
// [Starter] are started in dependency order; values that implement [Stopper]
// are stopped in reverse dependency order during [Container.Stop].
type Container struct {
	mu        sync.RWMutex
	providers map[string]*provider
	keys      []string // registration order

	// buildOrder records the order in which providers were actually
	// constructed. Filled during Start, used for Start/Stop ordering.
	buildOrder []string
	// buildStack is a transient cycle-detection stack, only used during
	// the build phase of Start.
	buildStack []string

	state       State
	stopTimeout time.Duration

	onStart    []func(ctx context.Context) error
	onStarted  []func(ctx context.Context) error
	onStopping []func(ctx context.Context) error
	onStop     []func(ctx context.Context) error
}

// New creates an empty Container.
func New(opts ...Option) *Container {
	c := &Container{
		providers:   make(map[string]*provider),
		stopTimeout: 30 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// Provide registers a lazily-invoked constructor under key.
// The constructor is called during [Container.Start]; its dependencies are
// resolved automatically when it calls [Get] on other keys.
func Provide[T any](c *Container, key string, ctor func(*Container) (T, error)) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidKey)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != StateNew && c.state != StateStopped {
		return fmt.Errorf("%w: current state is %s", ErrContainerNotIdle, c.state)
	}
	if _, exists := c.providers[key]; exists {
		return fmt.Errorf("%w: %s", ErrComponentExists, key)
	}

	c.providers[key] = &provider{
		key: key,
		constructor: func(cont *Container) (any, error) {
			return ctor(cont)
		},
	}
	c.keys = append(c.keys, key)
	return nil
}

// MustProvide is like [Provide] but panics on error.
func MustProvide[T any](c *Container, key string, ctor func(*Container) (T, error)) {
	if err := Provide(c, key, ctor); err != nil {
		panic(fmt.Sprintf("cx: MustProvide: %v", err))
	}
}

// Supply registers a pre-constructed value under key.
// Internally it wraps the value in a constructor so it participates in the
// same build lifecycle as [Provide]-registered components.
func Supply[T any](c *Container, key string, value T) error {
	return Provide(c, key, func(_ *Container) (T, error) {
		return value, nil
	})
}

// MustSupply is like [Supply] but panics on error.
func MustSupply[T any](c *Container, key string, value T) {
	if err := Supply(c, key, value); err != nil {
		panic(fmt.Sprintf("cx: MustSupply: %v", err))
	}
}

// ---------------------------------------------------------------------------
// Retrieval
// ---------------------------------------------------------------------------

// Get returns the value registered under key, cast to type T.
//
// During [Container.Start] (StateStarting) an unbuilt dependency is
// constructed on the fly (lazy build). After Start (StateRunning) the
// cached value is returned directly.
func Get[T any](c *Container, key string) (T, error) {
	var zero T

	c.mu.RLock()
	p, exists := c.providers[key]
	state := c.state
	built := exists && p.built
	var val any
	if built {
		val = p.value
	}
	c.mu.RUnlock()

	if !exists {
		return zero, fmt.Errorf("%w: %s", ErrComponentNotFound, key)
	}

	if !built {
		if state == StateStarting {
			if err := c.build(key); err != nil {
				return zero, err
			}
			val = p.value
		} else {
			return zero, fmt.Errorf("%w: %s is not built (state: %s)", ErrComponentNotFound, key, state)
		}
	}

	t, ok := val.(T)
	if !ok {
		return zero, fmt.Errorf("%w: key %q stores %T", ErrTypeMismatch, key, val)
	}
	return t, nil
}

// MustGet is like [Get] but panics on error.
func MustGet[T any](c *Container, key string) T {
	val, err := Get[T](c, key)
	if err != nil {
		panic(fmt.Sprintf("cx: MustGet: %v", err))
	}
	return val
}

// ---------------------------------------------------------------------------
// Build (internal, single-goroutine during Start)
// ---------------------------------------------------------------------------

func (c *Container) build(key string) error {
	p := c.providers[key]
	if p.built {
		return nil
	}

	// Cycle detection: if key is already on the build stack we have a cycle.
	for i, k := range c.buildStack {
		if k == key {
			cycle := make([]string, len(c.buildStack[i:])+1)
			copy(cycle, c.buildStack[i:])
			cycle[len(cycle)-1] = key
			return fmt.Errorf("%w: %s", ErrCircularDependency, strings.Join(cycle, " → "))
		}
	}

	c.buildStack = append(c.buildStack, key)
	val, err := p.constructor(c)
	c.buildStack = c.buildStack[:len(c.buildStack)-1]

	if err != nil {
		return fmt.Errorf("construct %s: %w", key, err)
	}

	p.value = val
	p.built = true
	c.buildOrder = append(c.buildOrder, key)
	return nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start constructs all registered components and starts them in dependency
// order. Hooks: onStart → Starter.Start (dependency order) → onStarted.
//
// If any component fails to start, already-started components are stopped
// in reverse order (best-effort) before returning the error.
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.state != StateNew && c.state != StateStopped {
		state := c.state
		c.mu.Unlock()
		return fmt.Errorf("cx: cannot start in state %s", state)
	}
	c.state = StateStarting
	c.buildOrder = c.buildOrder[:0]
	c.buildStack = c.buildStack[:0]
	keys := make([]string, len(c.keys))
	copy(keys, c.keys)
	c.mu.Unlock()

	// ---- Build phase (single goroutine, no locks needed) ----
	for _, key := range keys {
		if err := c.build(key); err != nil {
			c.mu.Lock()
			c.state = StateFailed
			c.mu.Unlock()
			return fmt.Errorf("cx: %w", err)
		}
	}

	// ---- onStart hooks ----
	for _, fn := range c.onStart {
		if err := fn(ctx); err != nil {
			c.mu.Lock()
			c.state = StateFailed
			c.mu.Unlock()
			return fmt.Errorf("cx: onStart hook: %w", err)
		}
	}

	// ---- Start phase (dependency order) ----
	type started struct {
		key  string
		stop func(context.Context) error
	}
	var startedComps []started

	rollback := func() {
		for i := len(startedComps) - 1; i >= 0; i-- {
			s := startedComps[i]
			stopCtx, cancel := context.WithTimeout(context.Background(), c.stopTimeout)
			_ = s.stop(stopCtx) // best-effort
			cancel()
		}
	}

	for _, key := range c.buildOrder {
		p := c.providers[key]
		if s, ok := p.value.(Starter); ok {
			if err := s.Start(ctx); err != nil {
				rollback()
				c.mu.Lock()
				c.state = StateFailed
				c.mu.Unlock()
				return fmt.Errorf("cx: start %s: %w", key, err)
			}
		}
		if s, ok := p.value.(Stopper); ok {
			startedComps = append(startedComps, started{key: key, stop: s.Stop})
		}
	}

	// ---- onStarted hooks ----
	for _, fn := range c.onStarted {
		if err := fn(ctx); err != nil {
			rollback()
			c.mu.Lock()
			c.state = StateFailed
			c.mu.Unlock()
			return fmt.Errorf("cx: onStarted hook: %w", err)
		}
	}

	c.mu.Lock()
	c.state = StateRunning
	c.mu.Unlock()
	return nil
}

// Stop stops all components in reverse dependency order.
// Hooks: onStopping → Stopper.Stop (reverse) → onStop.
// Errors are collected and returned as a joined error.
func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.state != StateRunning && c.state != StateFailed {
		state := c.state
		c.mu.Unlock()
		return fmt.Errorf("cx: cannot stop in state %s", state)
	}
	c.state = StateStopping
	c.mu.Unlock()

	var errs []error

	// onStopping hooks
	for _, fn := range c.onStopping {
		if err := fn(ctx); err != nil {
			errs = append(errs, fmt.Errorf("cx: onStopping hook: %w", err))
		}
	}

	// Stop in reverse build order
	for i := len(c.buildOrder) - 1; i >= 0; i-- {
		key := c.buildOrder[i]
		p := c.providers[key]
		if s, ok := p.value.(Stopper); ok {
			stopCtx, cancel := context.WithTimeout(ctx, c.stopTimeout)
			if err := s.Stop(stopCtx); err != nil {
				errs = append(errs, fmt.Errorf("cx: stop %s: %w", key, err))
			}
			cancel()
		}
	}

	// onStop hooks
	for _, fn := range c.onStop {
		if err := fn(ctx); err != nil {
			errs = append(errs, fmt.Errorf("cx: onStop hook: %w", err))
		}
	}

	// Reset build state for potential Restart.
	c.mu.Lock()
	c.state = StateStopped
	for _, p := range c.providers {
		p.built = false
		p.value = nil
	}
	c.buildOrder = c.buildOrder[:0]
	c.mu.Unlock()

	return errors.Join(errs...)
}

// Restart stops then starts the container.
func (c *Container) Restart(ctx context.Context) error {
	if err := c.Stop(ctx); err != nil {
		return err
	}
	return c.Start(ctx)
}

// ---------------------------------------------------------------------------
// Health check / Metrics
// ---------------------------------------------------------------------------

// HealthCheck runs Check on every value that implements [Checker] and returns
// an aggregated report.
func (c *Container) HealthCheck(ctx context.Context) *HealthReport {
	c.mu.RLock()
	order := make([]string, len(c.buildOrder))
	copy(order, c.buildOrder)
	c.mu.RUnlock()

	report := &HealthReport{Healthy: true}
	for _, key := range order {
		c.mu.RLock()
		p := c.providers[key]
		val := p.value
		c.mu.RUnlock()

		ch := ComponentHealth{Key: key, Healthy: true}
		if checker, ok := val.(Checker); ok {
			if err := checker.Check(ctx); err != nil {
				ch.Healthy = false
				ch.Error = err
				report.Healthy = false
			}
		}
		report.Components = append(report.Components, ch)
	}
	return report
}

// Metrics returns basic container metrics.
func (c *Container) Metrics() *ContainerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &ContainerMetrics{
		ComponentCount: len(c.providers),
		State:          c.state,
	}
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

// Keys returns all registered keys in registration order.
func (c *Container) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.keys))
	copy(out, c.keys)
	return out
}

// Has reports whether a key is registered.
func (c *Container) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.providers[key]
	return exists
}

// Count returns the total number of registered components.
func (c *Container) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.providers)
}

// State returns the current lifecycle state.
func (c *Container) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}
