package cx

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"
)

type buildFlight struct {
	done chan struct{}
	err  error
}

type provider struct {
	name         string
	typ          reflect.Type
	constructor  func(*Container) (any, error)
	value        any
	built        bool
	owned        bool
	declaredDeps []string
	deps         []string
}

type buildRound struct {
	open    bool
	wg      sync.WaitGroup
	flights map[string]*buildFlight
	waiting map[string]map[string]int
}

type containerCore struct {
	mu         sync.RWMutex
	providers  map[string]*provider
	keys       []string
	buildOrder []string
	state      State
	buildGen   uint64
	round      *buildRound

	shutdownTimeout      time.Duration
	componentStopTimeout time.Duration
	rollbackTimeout      time.Duration
	healthTimeout        time.Duration

	onStart    []func(context.Context) error
	onStarted  []func(context.Context) error
	onStopping []func(context.Context) error
	onStop     []func(context.Context) error

	onStoppingDone []bool
	onStopDone     []bool
	supervisor     *supervisor
	healthFlights  map[string]*healthFlight
}

// Container manages typed component registration, dependency ordering, lifecycle
// hooks, rollback, health probes, and supervised runners.
type Container struct {
	*containerCore
	stack []string // immutable per-build call path; never mutates shared core state
}

// Option configures a Container.
type Option func(*Container)

// WithShutdownTimeout bounds the complete shutdown sequence.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.shutdownTimeout = timeout
		}
	}
}

// WithComponentStopTimeout bounds each individual component Stop call.
func WithComponentStopTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.componentStopTimeout = timeout
		}
	}
}

// WithRollbackTimeout bounds cleanup after startup failure.
func WithRollbackTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.rollbackTimeout = timeout
		}
	}
}

// WithHealthTimeout bounds each component health check.
func WithHealthTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.healthTimeout = timeout
		}
	}
}

func WithOnStart(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStart })
}

func WithOnStarted(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStarted })
}

func WithOnStopping(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStopping })
}

func WithOnStop(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStop })
}

func appendHook(
	fn func(context.Context) error,
	selectHooks func(*Container) *[]func(context.Context) error,
) Option {
	return func(c *Container) {
		if fn != nil {
			hooks := selectHooks(c)
			*hooks = append(*hooks, fn)
		}
	}
}

// ProvideOption configures a component registration.
type ProvideOption func(*provider) error

// DependsOn declares lifecycle dependencies that are not constructor inputs.
func DependsOn(dependencies ...Dependency) ProvideOption {
	return func(p *provider) error {
		for _, dep := range dependencies {
			if dep == nil || dep.dependencyName() == "" {
				return ErrInvalidDependency
			}
			p.declaredDeps = append(p.declaredDeps, dep.dependencyName())
		}
		return nil
	}
}

// New constructs an idle Container.
func New(opts ...Option) *Container {
	c := &Container{
		containerCore: &containerCore{
			providers:            make(map[string]*provider),
			shutdownTimeout:      30 * time.Second,
			componentStopTimeout: 10 * time.Second,
			rollbackTimeout:      30 * time.Second,
			healthTimeout:        10 * time.Second,
			healthFlights:        make(map[string]*healthFlight),
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func (c *Container) Provide[T any](
	key Key[T],
	constructor func(*Container) (T, error),
	options ...ProvideOption,
) error {
	if c == nil || c.containerCore == nil {
		return ErrNilContainer
	}
	if key.name == "" {
		return ErrInvalidKey
	}
	if constructor == nil {
		return fmt.Errorf("%w: %s", ErrNilConstructor, key.name)
	}

	p := &provider{
		name: key.name,
		typ:  reflect.TypeFor[T](),
		constructor: func(sc *Container) (any, error) {
			return constructor(sc)
		},
	}
	for _, option := range options {
		if option != nil {
			if err := option(p); err != nil {
				return fmt.Errorf("configure %s: %w", key.name, err)
			}
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.idleLocked() {
		return fmt.Errorf("%w: %s", ErrContainerNotIdle, c.state)
	}
	if _, exists := c.providers[key.name]; exists {
		return fmt.Errorf("%w: %s", ErrComponentExists, key.name)
	}
	c.providers[key.name] = p
	c.keys = append(c.keys, key.name)
	return nil
}

func (c *Container) MustProvide[T any](
	key Key[T],
	constructor func(*Container) (T, error),
	options ...ProvideOption,
) {
	if err := c.Provide(key, constructor, options...); err != nil {
		panic(err)
	}
}

func (c *Container) Supply[T any](key Key[T], value T, options ...ProvideOption) error {
	return c.Provide(key, func(*Container) (T, error) {
		return value, nil
	}, options...)
}

func (c *Container) MustSupply[T any](key Key[T], value T, options ...ProvideOption) {
	if err := c.Supply(key, value, options...); err != nil {
		panic(err)
	}
}

func (c *Container) Get[T any](key Key[T]) (T, error) {
	var zero T
	if c == nil || c.containerCore == nil {
		return zero, ErrNilContainer
	}

	wantType := reflect.TypeFor[T]()
	c.mu.RLock()
	p, exists := c.providers[key.name]
	state := c.state
	if !exists {
		c.mu.RUnlock()
		return zero, fmt.Errorf("%w: %s", ErrComponentNotFound, key.name)
	}
	if p.typ != wantType {
		c.mu.RUnlock()
		return zero, fmt.Errorf("%w: %s", ErrTypeMismatch, key.name)
	}
	if p.built {
		value := p.value
		c.mu.RUnlock()
		if len(c.stack) > 0 {
			c.mu.Lock()
			c.recordDependencyLocked(key.name, c.stack)
			c.mu.Unlock()
		}
		if value == nil {
			return zero, nil
		}
		result, ok := value.(T)
		if !ok {
			return zero, fmt.Errorf("%w: %s", ErrTypeMismatch, key.name)
		}
		return result, nil
	}
	c.mu.RUnlock()

	if state != StateStarting {
		return zero, fmt.Errorf("%w: %s is not built in %s", ErrComponentNotFound, key.name, state)
	}
	if err := c.build(key.name, c.stack); err != nil {
		return zero, err
	}

	c.mu.RLock()
	value := p.value
	c.mu.RUnlock()
	if value == nil {
		return zero, nil
	}
	result, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrTypeMismatch, key.name)
	}
	return result, nil
}

func (c *Container) MustGet[T any](key Key[T]) T {
	value, err := c.Get(key)
	if err != nil {
		panic(err)
	}
	return value
}

func (c *Container) Has[T any](key Key[T]) bool {
	if c == nil || c.containerCore == nil {
		return false
	}
	wantType := reflect.TypeFor[T]()
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, exists := c.providers[key.name]
	return exists && p.typ == wantType
}

func (c *Container) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.keys)
}

func (c *Container) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.providers)
}

func (c *Container) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Container) Metrics() ContainerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ContainerMetrics{
		ComponentCount: len(c.providers),
		State:          c.state,
	}
}

func (c *Container) DependencyGraph() map[string][]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	graph := make(map[string][]string, len(c.providers))
	for name, p := range c.providers {
		graph[name] = slices.Clone(p.deps)
	}
	return graph
}

func (c *Container) idleLocked() bool {
	return c.state == StateNew || c.state == StateStopped || c.state == StateFailed
}

func (c *Container) resetInstancesLocked() {
	for _, p := range c.providers {
		p.value = nil
		p.built = false
		p.owned = false
		p.deps = nil
	}
	c.buildOrder = nil
	c.onStoppingDone = make([]bool, len(c.onStopping))
	c.onStopDone = make([]bool, len(c.onStop))
	c.healthFlights = make(map[string]*healthFlight)
	c.round = nil
}
