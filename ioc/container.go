package ioc

import (
	"context"
	"maps"
	"time"
)

// Default namespace constants
const (
	ConfigNamespace        = "config"
	DatabaseNamespace      = "database"
	BaseComponentNamespace = "baseComponent"
	HandlerNamespace       = "handler"
	ControllerNamespace    = "controller"
)

// DefaultNamespaceOrders defines the default order for namespaces
var DefaultNamespaceOrders = map[string]int{
	ConfigNamespace:        -100,
	DatabaseNamespace:      -80,
	BaseComponentNamespace: -60,
	HandlerNamespace:       -40,
	ControllerNamespace:    -20,
}

// ContainerBuilder provides a fluent interface for building IoC containers.
type ContainerBuilder struct {
	namespaces      map[string]int
	hooks           *LifecycleHooks
	shutdownTimeout time.Duration
	verbose         bool
}

// NewContainerBuilder creates a new container builder.
func NewContainerBuilder() *ContainerBuilder {
	return &ContainerBuilder{
		namespaces:      make(map[string]int),
		hooks:           &LifecycleHooks{},
		shutdownTimeout: 30 * time.Second,
	}
}

// WithDefaultNamespaces adds the default namespaces to the builder.
func (cb *ContainerBuilder) WithDefaultNamespaces() *ContainerBuilder {
	maps.Copy(cb.namespaces, DefaultNamespaceOrders)
	return cb
}

// AddNamespace adds a namespace with the specified order.
func (cb *ContainerBuilder) AddNamespace(name string, order int) *ContainerBuilder {
	cb.namespaces[name] = order
	return cb
}

// WithShutdownTimeout sets the shutdown timeout.
func (cb *ContainerBuilder) WithShutdownTimeout(timeout time.Duration) *ContainerBuilder {
	cb.shutdownTimeout = timeout
	return cb
}

// Withverbose enables or disables verbose logging for the IoC container.
// When enabled, the container will log component registration, initialization, and shutdown activities.
func (cb *ContainerBuilder) WithVerbose(enabled bool) *ContainerBuilder {
	cb.verbose = enabled
	return cb
}

// AddBeforeInitHook adds a before-init lifecycle hook.
func (cb *ContainerBuilder) AddBeforeInitHook(hook func(ctx context.Context, container *Store) error) *ContainerBuilder {
	cb.hooks.BeforeInit = append(cb.hooks.BeforeInit, hook)
	return cb
}

// AddAfterInitHook adds an after-init lifecycle hook.
func (cb *ContainerBuilder) AddAfterInitHook(hook func(ctx context.Context, container *Store) error) *ContainerBuilder {
	cb.hooks.AfterInit = append(cb.hooks.AfterInit, hook)
	return cb
}

// AddBeforeDestroyHook adds a before-destroy lifecycle hook.
func (cb *ContainerBuilder) AddBeforeDestroyHook(hook func(ctx context.Context, container *Store) error) *ContainerBuilder {
	cb.hooks.BeforeDestroy = append(cb.hooks.BeforeDestroy, hook)
	return cb
}

// AddAfterDestroyHook adds an after-destroy lifecycle hook.
func (cb *ContainerBuilder) AddAfterDestroyHook(hook func(ctx context.Context, container *Store) error) *ContainerBuilder {
	cb.hooks.AfterDestroy = append(cb.hooks.AfterDestroy, hook)
	return cb
}

// Build creates and returns a new container with lifecycle manager.
func (cb *ContainerBuilder) Build() (*Store, *Lifecycle) {
	store := NewStore(
		WithNamespaces(cb.namespaces),
		WithHooks(cb.hooks),
		WithVerbose(cb.verbose),
	)

	lifecycle := NewLifecycle(store, cb.shutdownTimeout)

	return store, lifecycle
}

// ApplicationContainerOption represents a functional option for ApplicationContainer configuration.
type ApplicationContainerOption func(*ContainerBuilder)

// WithApplicationLogging enables verbose logging for the application container.
func WithApplicationVerbose() ApplicationContainerOption {
	return func(cb *ContainerBuilder) {
		cb.verbose = true
	}
}

// WithApplicationShutdownTimeout sets the shutdown timeout for the application container.
func WithApplicationShutdownTimeout(timeout time.Duration) ApplicationContainerOption {
	return func(cb *ContainerBuilder) {
		cb.shutdownTimeout = timeout
	}
}

// WithApplicationNamespace adds a custom namespace to the application container.
func WithApplicationNamespace(name string, order int) ApplicationContainerOption {
	return func(cb *ContainerBuilder) {
		cb.namespaces[name] = order
	}
}

// WithApplicationHooks sets lifecycle hooks for the application container.
func WithApplicationHooks(hooks *LifecycleHooks) ApplicationContainerOption {
	return func(cb *ContainerBuilder) {
		cb.hooks = hooks
	}
}

// ApplicationContainer provides a high-level interface for application container management.
type ApplicationContainer struct {
	store     *Store
	lifecycle *Lifecycle
}

// Ac is the global application container instance.
var (
	Ac *ApplicationContainer
)

// Initialize the global application container with default settings.
func init() {
	Ac = NewApplicationContainer()
}

// NewApplicationContainer creates a new application container with default configuration.
// It accepts optional configuration functions to customize the container behavior.
func NewApplicationContainer(options ...ApplicationContainerOption) *ApplicationContainer {
	builder := NewContainerBuilder().
		WithDefaultNamespaces()

	// Apply all provided options
	for _, option := range options {
		option(builder)
	}

	store, lifecycle := builder.Build()

	return &ApplicationContainer{
		store:     store,
		lifecycle: lifecycle,
	}
}

// NewCustomApplicationContainer creates a new application container with custom configuration.
func NewCustomApplicationContainer(builder *ContainerBuilder) *ApplicationContainer {
	store, lifecycle := builder.Build()

	return &ApplicationContainer{
		store:     store,
		lifecycle: lifecycle,
	}
}

// RegisterConfig registers a configuration component.
func (ac *ApplicationContainer) RegisterConfig(component Component, order ...int) error {
	return ac.store.Register(ConfigNamespace, component, order...)
}

// RegisterDatabase registers a database component.
func (ac *ApplicationContainer) RegisterDatabase(component Component, order ...int) error {
	return ac.store.Register(DatabaseNamespace, component, order...)
}

// RegisterBaseComponent registers a base component.
func (ac *ApplicationContainer) RegisterBaseComponent(component Component, order ...int) error {
	return ac.store.Register(BaseComponentNamespace, component, order...)
}

// RegisterHandler registers a handler component.
func (ac *ApplicationContainer) RegisterHandler(component Component, order ...int) error {
	return ac.store.Register(HandlerNamespace, component, order...)
}

// RegisterController registers a controller component.
func (ac *ApplicationContainer) RegisterController(component Component, order ...int) error {
	return ac.store.Register(ControllerNamespace, component, order...)
}

// Register registers a component in the specified namespace.
func (ac *ApplicationContainer) Register(namespace string, component Component, order ...int) error {
	return ac.store.Register(namespace, component, order...)
}

// Get retrieves a component by name from the specified namespace.
func (ac *ApplicationContainer) Get(namespace, name string) Component {
	return ac.store.Get(namespace, name)
}

// GetConfig retrieves a configuration component by name.
func (ac *ApplicationContainer) GetConfig(name string) Component {
	return ac.store.Get(ConfigNamespace, name)
}

// GetDatabase retrieves a database component by name.
func (ac *ApplicationContainer) GetDatabase(name string) Component {
	return ac.store.Get(DatabaseNamespace, name)
}

// GetBaseComponent retrieves a base component by name.
func (ac *ApplicationContainer) GetBaseComponent(name string) Component {
	return ac.store.Get(BaseComponentNamespace, name)
}

// GetHandler retrieves a handler component by name.
func (ac *ApplicationContainer) GetHandler(name string) Component {
	return ac.store.Get(HandlerNamespace, name)
}

// GetController retrieves a controller component by name.
func (ac *ApplicationContainer) GetController(name string) Component {
	return ac.store.Get(ControllerNamespace, name)
}

// Initialize initializes the container.
func (ac *ApplicationContainer) Initialize(ctx context.Context) error {
	return ac.lifecycle.Initialize(ctx)
}

// Shutdown shuts down the container.
func (ac *ApplicationContainer) Shutdown(ctx context.Context) error {
	return ac.lifecycle.Shutdown(ctx)
}

// Restart restarts the container.
func (ac *ApplicationContainer) Restart(ctx context.Context) error {
	return ac.lifecycle.Restart(ctx)
}

// GetState returns the current lifecycle state.
func (ac *ApplicationContainer) GetState() LifecycleState {
	return ac.lifecycle.GetState()
}

// HealthCheck performs a health check.
func (ac *ApplicationContainer) HealthCheck(ctx context.Context) *HealthReport {
	return ac.lifecycle.HealthCheck(ctx)
}

// GetMetrics returns container metrics.
func (ac *ApplicationContainer) GetMetrics(ctx context.Context) *ContainerMetrics {
	return ac.lifecycle.GetMetrics(ctx)
}

// Store returns the underlying store (for advanced usage).
func (ac *ApplicationContainer) Store() *Store {
	return ac.store
}

// Lifecycle returns the underlying lifecycle manager (for advanced usage).
func (ac *ApplicationContainer) Lifecycle() *Lifecycle {
	return ac.lifecycle
}
