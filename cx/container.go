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
type State int32

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

// WithHealthTimeout sets the per-component timeout used during HealthCheck.
func WithHealthTimeout(d time.Duration) Option {
	return func(c *Container) { c.healthTimeout = d }
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
	deps        []string // keys this provider depends on (recorded during build)
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
//
// Goroutine safety:
//   - Registration (Provide/Supply) and retrieval (Get) are safe for
//     concurrent use after Start has returned.
//   - During Start, only the goroutine that called Start may interact with
//     the container (constructors call Get on the same goroutine). Spawning
//     goroutines that call Get inside a constructor is not supported.
type Container struct {
	mu        sync.RWMutex
	providers map[string]*provider
	keys      []string // registration order

	// buildOrder records the order in which providers were actually
	// constructed. Filled during Start, used for Start/Stop ordering.
	buildOrder []string
	// buildStack is the cycle-detection stack used during the build phase.
	// Only the Start goroutine writes to it. The top of the stack also acts
	// as the "current caller" used to record dependency edges.
	buildStack []string

	state         State
	stopTimeout   time.Duration
	healthTimeout time.Duration

	onStart    []func(ctx context.Context) error
	onStarted  []func(ctx context.Context) error
	onStopping []func(ctx context.Context) error
	onStop     []func(ctx context.Context) error
}

// New creates an empty Container.
func New(opts ...Option) *Container {
	c := &Container{
		providers:     make(map[string]*provider),
		stopTimeout:   30 * time.Second,
		healthTimeout: 10 * time.Second,
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
	if ctor == nil {
		return fmt.Errorf("cx: nil constructor for %q", key)
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

// Supply registers a pre-constructed value under key.
// Internally it wraps the value in a constructor so it participates in the
// same build lifecycle as [Provide]-registered components.
func Supply[T any](c *Container, key string, value T) error {
	return Provide(c, key, func(_ *Container) (T, error) {
		return value, nil
	})
}

// ---------------------------------------------------------------------------
// Retrieval
// ---------------------------------------------------------------------------

// Get returns the value registered under key, cast to type T.
//
// During [Container.Start] (StateStarting) an unbuilt dependency is
// constructed on the fly (lazy build). After Start (StateRunning) the
// cached value is returned directly.
//
// Errors:
//   - ErrComponentNotFound: key is not registered, or is registered but not
//     yet built (Get called outside Start).
//   - ErrTypeMismatch: stored value cannot be cast to T.
//   - ErrCircularDependency / constructor error: bubbled from lazy build.
func Get[T any](c *Container, key string) (T, error) {
	var zero T

	c.mu.RLock()
	p, exists := c.providers[key]
	state := c.state
	c.mu.RUnlock()

	if !exists {
		return zero, fmt.Errorf("%w: %s", ErrComponentNotFound, key)
	}

	// Fast path: already built. Read under RLock to be safe vs. concurrent Stop.
	c.mu.RLock()
	built := p.built
	val := p.value
	c.mu.RUnlock()

	if !built {
		if state != StateStarting {
			return zero, fmt.Errorf("%w: %s is not built (state: %s)", ErrComponentNotFound, key, state)
		}
		// Lazy build – called from inside a constructor (Start goroutine).
		// Record dependency edge: top-of-stack -> key.
		if err := c.build(key); err != nil {
			return zero, err
		}
		c.mu.RLock()
		val = p.value
		c.mu.RUnlock()
	}

	t, ok := val.(T)
	if !ok {
		return zero, fmt.Errorf("%w: key %q stores %T, requested %T", ErrTypeMismatch, key, val, zero)
	}
	return t, nil
}

// ---------------------------------------------------------------------------
// Build (internal, runs in the Start goroutine)
// ---------------------------------------------------------------------------

// build constructs the provider for key. It is safe to call recursively
// from constructors via Get. The Start goroutine drives the build phase;
// concurrent calls from other goroutines are not supported (see Container
// docs).
//
// Locking strategy: mu is held only for short critical sections (state
// reads/writes, slice mutations). The user constructor runs without the
// lock so it can recursively Get other components.
func (c *Container) build(key string) (retErr error) {
	c.mu.Lock()
	p, ok := c.providers[key]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrComponentNotFound, key)
	}
	if p.built {
		// Already built – just record the dep edge from caller (if any).
		c.recordDepEdgeLocked(key)
		c.mu.Unlock()
		return nil
	}

	// Cycle detection on the build stack.
	for i, k := range c.buildStack {
		if k == key {
			cycle := make([]string, len(c.buildStack[i:])+1)
			copy(cycle, c.buildStack[i:])
			cycle[len(cycle)-1] = key
			c.mu.Unlock()
			return fmt.Errorf("%w: %s", ErrCircularDependency, strings.Join(cycle, " → "))
		}
	}

	// Record dep edge: caller (top of stack) depends on this key.
	c.recordDepEdgeLocked(key)

	c.buildStack = append(c.buildStack, key)
	ctor := p.constructor
	c.mu.Unlock()

	// Run the constructor without holding the lock so it can recursively Get.
	var val any
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("cx: panic constructing %s: %v", key, r)
			}
		}()
		val, err = ctor(c)
	}()

	c.mu.Lock()
	// Pop our entry off the build stack (regardless of error).
	if n := len(c.buildStack); n > 0 && c.buildStack[n-1] == key {
		c.buildStack = c.buildStack[:n-1]
	}
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("construct %s: %w", key, err)
	}
	p.value = val
	p.built = true
	c.buildOrder = append(c.buildOrder, key)
	c.mu.Unlock()
	return nil
}

// recordDepEdgeLocked appends key to the top-of-stack provider's deps list
// if there is a current caller. mu must be held by the caller.
func (c *Container) recordDepEdgeLocked(key string) {
	n := len(c.buildStack)
	if n == 0 {
		return
	}
	caller := c.providers[c.buildStack[n-1]]
	if caller == nil {
		return
	}
	for _, d := range caller.deps {
		if d == key {
			return
		}
	}
	caller.deps = append(caller.deps, key)
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start constructs all registered components and starts them in dependency
// order. Hooks: onStart → Starter.Start (dependency order) → onStarted.
//
// If any component fails to start, already-started components are stopped
// in reverse order (best-effort) before returning the error.
//
// Start respects ctx cancellation: if ctx is cancelled during the build
// phase, the next build step returns ctx.Err.
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
	// Reset deps from any previous run.
	for _, p := range c.providers {
		p.deps = nil
	}
	keys := make([]string, len(c.keys))
	copy(keys, c.keys)
	c.mu.Unlock()

	setFailed := func() {
		c.mu.Lock()
		c.state = StateFailed
		c.mu.Unlock()
	}

	// ---- Build phase ----
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			setFailed()
			return fmt.Errorf("cx: build cancelled: %w", err)
		}
		if err := c.build(key); err != nil {
			setFailed()
			return fmt.Errorf("cx: %w", err)
		}
	}

	// ---- onStart hooks ----
	for _, fn := range c.onStart {
		if err := fn(ctx); err != nil {
			setFailed()
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

	c.mu.RLock()
	order := make([]string, len(c.buildOrder))
	copy(order, c.buildOrder)
	c.mu.RUnlock()

	for _, key := range order {
		c.mu.RLock()
		p := c.providers[key]
		val := p.value
		c.mu.RUnlock()

		if s, ok := val.(Starter); ok {
			if err := s.Start(ctx); err != nil {
				rollback()
				setFailed()
				return fmt.Errorf("cx: start %s: %w", key, err)
			}
		}
		if s, ok := val.(Stopper); ok {
			startedComps = append(startedComps, started{key: key, stop: s.Stop})
		}
	}

	// ---- onStarted hooks ----
	for _, fn := range c.onStarted {
		if err := fn(ctx); err != nil {
			rollback()
			setFailed()
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
	order := make([]string, len(c.buildOrder))
	copy(order, c.buildOrder)
	c.mu.Unlock()

	var errs []error

	// onStopping hooks
	for _, fn := range c.onStopping {
		if err := fn(ctx); err != nil {
			errs = append(errs, fmt.Errorf("cx: onStopping hook: %w", err))
		}
	}

	// Stop in reverse build order.
	for i := len(order) - 1; i >= 0; i-- {
		key := order[i]
		c.mu.RLock()
		p := c.providers[key]
		val := p.value
		c.mu.RUnlock()
		if s, ok := val.(Stopper); ok {
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
		p.deps = nil
	}
	c.buildOrder = c.buildOrder[:0]
	c.mu.Unlock()

	return errors.Join(errs...)
}

// Restart stops then starts the container. Constructors are re-invoked.
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
// an aggregated report. Checks run concurrently; each check is bounded by the
// configured health timeout (see [WithHealthTimeout], default 10s).
func (c *Container) HealthCheck(ctx context.Context) HealthReport {
	c.mu.RLock()
	order := make([]string, len(c.buildOrder))
	copy(order, c.buildOrder)
	values := make([]any, len(order))
	for i, k := range order {
		values[i] = c.providers[k].value
	}
	timeout := c.healthTimeout
	c.mu.RUnlock()

	results := make([]ComponentHealth, len(order))
	var wg sync.WaitGroup
	for i := range order {
		ch := ComponentHealth{Key: order[i], Healthy: true}
		checker, ok := values[i].(Checker)
		if !ok {
			results[i] = ch
			continue
		}
		wg.Add(1)
		go func(idx int, key string, ck Checker) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			h := ComponentHealth{Key: key, Healthy: true}
			if err := ck.Check(cctx); err != nil {
				h.Healthy = false
				h.Error = err
			}
			results[idx] = h
		}(i, order[i], checker)
	}
	wg.Wait()

	report := HealthReport{Components: results, Healthy: true}
	for _, r := range results {
		if !r.Healthy {
			report.Healthy = false
			break
		}
	}
	return report
}

// Metrics returns basic container metrics.
func (c *Container) Metrics() ContainerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ContainerMetrics{
		ComponentCount: len(c.providers),
		State:          c.state,
	}
}

// DependencyGraph returns the dependency edges recorded during the most
// recent Start, as a map from component key to the keys it directly depends
// on. The map is empty before Start. Returned slices are copies.
func (c *Container) DependencyGraph() map[string][]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g := make(map[string][]string, len(c.providers))
	for k, p := range c.providers {
		if len(p.deps) == 0 {
			g[k] = nil
			continue
		}
		deps := make([]string, len(p.deps))
		copy(deps, p.deps)
		g[k] = deps
	}
	return g
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
