package ioc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/log"
)

var (
	// ErrNamespaceNotFound is returned when a namespace is not found.
	ErrNamespaceNotFound = errors.New("namespace not found")

	// ErrComponentNotFound is returned when a component is not found.
	ErrComponentNotFound = errors.New("component not found")

	// ErrComponentAlreadyExists is returned when trying to register a component that already exists.
	ErrComponentAlreadyExists = errors.New("component already exists")

	// ErrNamespaceAlreadyExists is returned when trying to register a namespace that already exists.
	ErrNamespaceAlreadyExists = errors.New("namespace already exists")

	// ErrInvalidComponent is returned when a component doesn't implement required interfaces.
	ErrInvalidComponent = errors.New("invalid component")

	// ErrDependencyNotFound is returned when a required dependency is not found.
	ErrDependencyNotFound = errors.New("dependency not found")

	// ErrCircularDependency is returned when a circular dependency is detected.
	ErrCircularDependency = errors.New("circular dependency detected")
)

// ComponentDescriptor describes a registered component with its metadata.
type ComponentDescriptor struct {
	Name      string
	Instance  Component
	Order     int
	Namespace string
}

// Store represents the IoC container with enhanced functionality.
// It manages multiple namespaces and provides type-safe dependency injection without reflection.
type Store struct {
	mu          sync.RWMutex
	namespaces  map[string]*Namespace
	sorted      []*Namespace
	dirty       bool
	initialized bool
	hooks       *LifecycleHooks
	verbose     bool
}

// LifecycleHooks provides hooks for container lifecycle events.
type LifecycleHooks struct {
	BeforeInit    []func(ctx context.Context, container *Store) error
	AfterInit     []func(ctx context.Context, container *Store) error
	BeforeDestroy []func(ctx context.Context, container *Store) error
	AfterDestroy  []func(ctx context.Context, container *Store) error
}

// Namespace represents a collection of components with priority.
// Components within a namespace are initialized in order.
type Namespace struct {
	mu         sync.RWMutex
	name       string
	components map[string]*ComponentDescriptor
	sorted     []*ComponentDescriptor
	order      int
	dirty      bool
}

// StoreOption represents a functional option for Store configuration.
type StoreOption func(*Store)

// Withverbose controls whether to output logs during IoC registration process.
// When enabled, logs will be output during component registration, initialization, and shutdown.
// When disabled, no logs will be output during IoC operations.
func WithVerbose(enabled bool) StoreOption {
	return func(s *Store) {
		s.verbose = enabled
	}
}

// WithHooks sets lifecycle hooks for the container.
func WithHooks(hooks *LifecycleHooks) StoreOption {
	return func(s *Store) {
		s.hooks = hooks
	}
}

// WithNamespaces initializes the container with predefined namespaces.
func WithNamespaces(namespaces map[string]int) StoreOption {
	return func(s *Store) {
		for name, order := range namespaces {
			s.namespaces[name] = &Namespace{
				name:       name,
				components: make(map[string]*ComponentDescriptor),
				order:      order,
				dirty:      true,
			}
		}
		s.dirty = true
	}
}

// NewStore creates a new IoC container with the given options.
func NewStore(opts ...StoreOption) *Store {
	store := &Store{
		namespaces: make(map[string]*Namespace),
		dirty:      true,
		hooks:      &LifecycleHooks{},
	}

	for _, opt := range opts {
		opt(store)
	}

	return store
}

// logf logs a message if verbose logging is enabled.
func (s *Store) logf(format string, args ...any) {
	if s.verbose {
		log.Infof(format, args...)
	}
}

// RegisterNamespace registers a new namespace with the specified order.
func (s *Store) RegisterNamespace(name string, order int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.namespaces[name]; exists {
		return fmt.Errorf("%w: %s", ErrNamespaceAlreadyExists, name)
	}

	s.namespaces[name] = &Namespace{
		name:       name,
		components: make(map[string]*ComponentDescriptor),
		order:      order,
		dirty:      true,
	}
	s.dirty = true

	s.logf("[ioc] registered namespace: %s (order: %d)", name, order)
	return nil
}

// Register registers a component in the specified namespace.
func (s *Store) Register(namespace string, component Component, order ...int) error {
	if component == nil {
		return fmt.Errorf("%w: component is nil", ErrInvalidComponent)
	}

	ns, err := s.getNamespace(namespace)
	if err != nil {
		return err
	}

	componentOrder := 0
	if len(order) > 0 {
		componentOrder = order[0]
	} else if orderable, ok := component.(Orderable); ok {
		componentOrder = orderable.Order()
	}

	ns.mu.Lock()
	defer ns.mu.Unlock()

	name := component.Name()
	if _, exists := ns.components[name]; exists {
		return fmt.Errorf("%w: %s in namespace %s", ErrComponentAlreadyExists, name, namespace)
	}

	descriptor := &ComponentDescriptor{
		Name:      name,
		Instance:  component,
		Order:     componentOrder,
		Namespace: namespace,
	}

	ns.components[name] = descriptor
	ns.dirty = true

	s.logf("[ioc] registered component: %s in namespace %s (order: %d)", name, namespace, componentOrder)
	return nil
}

// RegisterWithPriority registers a component with the specified priority (for backward compatibility).
func (s *Store) RegisterWithPriority(namespace string, component Component, priority int) error {
	return s.Register(namespace, component, priority)
}

// Get retrieves a component by name from the specified namespace.
func (s *Store) Get(namespace, name string) Component {
	component, _ := s.GetSafe(namespace, name)
	return component
}

// GetSafe retrieves a component by name from the specified namespace with error handling.
func (s *Store) GetSafe(namespace, name string) (Component, error) {
	ns, err := s.getNamespace(namespace)
	if err != nil {
		return nil, err
	}

	ns.mu.RLock()
	defer ns.mu.RUnlock()

	descriptor, exists := ns.components[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s in namespace %s", ErrComponentNotFound, name, namespace)
	}

	return descriptor.Instance, nil
}

// GetAll returns all components from the specified namespace.
func (s *Store) GetAll(namespace string) []Component {
	components, _ := s.GetAllSafe(namespace)
	return components
}

// GetAllSafe returns all components from the specified namespace with error handling.
func (s *Store) GetAllSafe(namespace string) ([]Component, error) {
	ns, err := s.getNamespace(namespace)
	if err != nil {
		return nil, err
	}

	ns.mu.RLock()
	defer ns.mu.RUnlock()

	if len(ns.components) == 0 {
		return nil, nil
	}

	components := make([]Component, 0, len(ns.components))
	for _, descriptor := range ns.components {
		components = append(components, descriptor.Instance)
	}

	return components, nil
}

// GetAllSorted returns all components from the specified namespace in order.
func (s *Store) GetAllSorted(namespace string) []Component {
	components, _ := s.GetAllSortedSafe(namespace)
	return components
}

// GetAllSortedSafe returns all components from the specified namespace in order with error handling.
func (s *Store) GetAllSortedSafe(namespace string) ([]Component, error) {
	ns, err := s.getNamespace(namespace)
	if err != nil {
		return nil, err
	}

	ns.mu.Lock()
	ns.ensureSorted()
	sorted := make([]*ComponentDescriptor, len(ns.sorted))
	copy(sorted, ns.sorted)
	ns.mu.Unlock()

	if len(sorted) == 0 {
		return nil, nil
	}

	components := make([]Component, 0, len(sorted))
	for _, descriptor := range sorted {
		components = append(components, descriptor.Instance)
	}

	return components, nil
}

// ListNamespaces returns all namespace names.
func (s *Store) ListNamespaces() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.namespaces) == 0 {
		return nil
	}

	names := make([]string, 0, len(s.namespaces))
	for name := range s.namespaces {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// HasNamespace checks if a namespace exists.
func (s *Store) HasNamespace(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.namespaces[name]
	return exists
}

// HasObject checks if a component exists in the specified namespace.
func (s *Store) HasObject(namespace, name string) bool {
	ns, err := s.getNamespace(namespace)
	if err != nil {
		return false
	}

	ns.mu.RLock()
	defer ns.mu.RUnlock()
	_, exists := ns.components[name]
	return exists
}

// Count returns the total number of components across all namespaces.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, ns := range s.namespaces {
		ns.mu.RLock()
		total += len(ns.components)
		ns.mu.RUnlock()
	}

	return total
}

// CountInNamespace returns the number of components in the specified namespace.
func (s *Store) CountInNamespace(namespace string) int {
	ns, err := s.getNamespace(namespace)
	if err != nil {
		return 0
	}

	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return len(ns.components)
}

// Init initializes all registered components in order.
func (s *Store) Init() error {
	return s.InitWithContext(context.Background())
}

// InitWithContext initializes all registered components with a context.
func (s *Store) InitWithContext(ctx context.Context) error {
	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		return nil // Already initialized
	}
	s.mu.Unlock()

	// Create a context with reasonable timeout if none is set
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Execute before init hooks
	if s.hooks != nil {
		for _, hook := range s.hooks.BeforeInit {
			if err := hook(ctx, s); err != nil {
				return fmt.Errorf("before init hook failed: %w", err)
			}
		}
	}

	// Get sorted namespaces
	s.mu.Lock()
	s.ensureSorted()
	sortedNamespaces := make([]*Namespace, len(s.sorted))
	copy(sortedNamespaces, s.sorted)
	s.mu.Unlock()

	// Initialize components in each namespace
	for _, ns := range sortedNamespaces {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := s.initNamespace(ctx, ns); err != nil {
				return err
			}
		}
	}

	// Execute after init hooks
	if s.hooks != nil {
		for _, hook := range s.hooks.AfterInit {
			if err := hook(ctx, s); err != nil {
				return fmt.Errorf("after init hook failed: %w", err)
			}
		}
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	return nil
}

// initNamespace initializes all components in a single namespace.
func (s *Store) initNamespace(ctx context.Context, ns *Namespace) error {
	// Get sorted components
	ns.mu.Lock()
	ns.ensureSorted()
	sortedComponents := make([]*ComponentDescriptor, len(ns.sorted))
	copy(sortedComponents, ns.sorted)
	ns.mu.Unlock()

	if len(sortedComponents) == 0 {
		return nil
	}

	// Initialize components and collect names for logging
	componentNames := make([]string, 0, len(sortedComponents))
	for _, descriptor := range sortedComponents {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if initializer, ok := descriptor.Instance.(Initializer); ok {
				if err := initializer.Init(ctx); err != nil {
					return fmt.Errorf("failed to initialize component %s in namespace %s: %w",
						descriptor.Name, ns.name, err)
				}
			}
			componentNames = append(componentNames, descriptor.Name)
		}
	}

	s.logf("[ioc] initialized namespace: %s | components: %v", ns.name, componentNames)
	return nil
}

// Shutdown gracefully shuts down all components that implement the Destroyer interface.
func (s *Store) Shutdown(ctx context.Context) error {
	// Create a context with reasonable timeout if none is set
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Execute before destroy hooks
	if s.hooks != nil {
		for _, hook := range s.hooks.BeforeDestroy {
			if err := hook(ctx, s); err != nil {
				return fmt.Errorf("before destroy hook failed: %w", err)
			}
		}
	}

	// Get sorted namespaces (reverse order for shutdown)
	s.mu.Lock()
	s.ensureSorted()
	sortedNamespaces := make([]*Namespace, len(s.sorted))
	copy(sortedNamespaces, s.sorted)
	s.mu.Unlock()

	// Shutdown components in reverse order
	for i := len(sortedNamespaces) - 1; i >= 0; i-- {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := s.shutdownNamespace(ctx, sortedNamespaces[i]); err != nil {
				return err
			}
		}
	}

	// Execute after destroy hooks
	if s.hooks != nil {
		for _, hook := range s.hooks.AfterDestroy {
			if err := hook(ctx, s); err != nil {
				return fmt.Errorf("after destroy hook failed: %w", err)
			}
		}
	}

	s.mu.Lock()
	s.initialized = false
	s.mu.Unlock()

	return nil
}

// shutdownNamespace shuts down all components in a single namespace.
func (s *Store) shutdownNamespace(ctx context.Context, ns *Namespace) error {
	// Get sorted components (reverse order for shutdown)
	ns.mu.Lock()
	ns.ensureSorted()
	sortedComponents := make([]*ComponentDescriptor, len(ns.sorted))
	copy(sortedComponents, ns.sorted)
	ns.mu.Unlock()

	if len(sortedComponents) == 0 {
		return nil
	}

	// Shutdown components in reverse order and collect names for logging
	destroyedComponents := make([]string, 0, len(sortedComponents))
	for i := len(sortedComponents) - 1; i >= 0; i-- {
		descriptor := sortedComponents[i]
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if destroyer, ok := descriptor.Instance.(Destroyer); ok {
				if err := destroyer.Destroy(ctx); err != nil {
					return fmt.Errorf("failed to destroy component %s in namespace %s: %w",
						descriptor.Name, ns.name, err)
				}
				destroyedComponents = append(destroyedComponents, descriptor.Name)
			}
		}
	}

	if len(destroyedComponents) > 0 {
		s.logf("[ioc] shutdown namespace: %s | components: %v", ns.name, destroyedComponents)
	}
	return nil
}

// GinRouterRegister registers all GinRouter components with the gin router.
func (s *Store) GinRouterRegister(r gin.IRouter) error {
	// Get all namespaces
	s.mu.RLock()
	namespaces := make([]*Namespace, 0, len(s.namespaces))
	for _, ns := range s.namespaces {
		namespaces = append(namespaces, ns)
	}
	s.mu.RUnlock()

	// Process each namespace
	for _, ns := range namespaces {
		if err := s.registerRoutersInNamespace(ns, r); err != nil {
			return fmt.Errorf("failed to register routes in namespace %s: %w", ns.name, err)
		}
	}
	return nil
}

// registerRoutersInNamespace registers all GinRouter components in a single namespace.
func (s *Store) registerRoutersInNamespace(ns *Namespace, r gin.IRouter) error {
	// Get all components from the namespace
	ns.mu.RLock()
	components := make([]*ComponentDescriptor, 0, len(ns.components))
	for _, descriptor := range ns.components {
		components = append(components, descriptor)
	}
	ns.mu.RUnlock()

	if len(components) == 0 {
		return nil
	}

	// Register routers and collect names for logging
	routers := make([]string, 0, len(components))
	for _, descriptor := range components {
		// Check if component implements GinRouter interface
		if router, ok := descriptor.Instance.(GinRouter); ok {
			router.RegisterGinRoutes(r)
			routers = append(routers, descriptor.Name)
		}
	}

	if len(routers) > 0 {
		s.logf("[gin] namespace: %s | routers: %v", ns.name, routers)
	}
	return nil
}

// HealthCheck performs health checks on all registered components.
func (s *Store) HealthCheck(ctx context.Context) error {
	// Get all namespaces
	s.mu.RLock()
	namespaces := make([]*Namespace, 0, len(s.namespaces))
	for _, ns := range s.namespaces {
		namespaces = append(namespaces, ns)
	}
	s.mu.RUnlock()

	// Check health of all components
	for _, ns := range namespaces {
		if err := s.healthCheckNamespace(ctx, ns); err != nil {
			return err
		}
	}

	return nil
}

// healthCheckNamespace performs health checks on all components in a single namespace.
func (s *Store) healthCheckNamespace(ctx context.Context, ns *Namespace) error {
	// Get all components from the namespace
	ns.mu.RLock()
	components := make([]*ComponentDescriptor, 0, len(ns.components))
	for _, descriptor := range ns.components {
		components = append(components, descriptor)
	}
	ns.mu.RUnlock()

	// Perform health checks
	for _, descriptor := range components {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if checker, ok := descriptor.Instance.(HealthChecker); ok {
				if err := checker.HealthCheck(ctx); err != nil {
					return fmt.Errorf("health check failed for %s in namespace %s: %w",
						descriptor.Name, ns.name, err)
				}
			}
		}
	}

	return nil
}

// getNamespace safely retrieves a namespace.
func (s *Store) getNamespace(name string) (*Namespace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ns, exists := s.namespaces[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNamespaceNotFound, name)
	}

	return ns, nil
}

// ensureSorted ensures the store is sorted if dirty.
func (s *Store) ensureSorted() {
	if s.dirty {
		s.sortNamespaces()
		s.dirty = false
	}
}

// sortNamespaces sorts namespaces by order.
func (s *Store) sortNamespaces() {
	if cap(s.sorted) >= len(s.namespaces) {
		s.sorted = s.sorted[:0]
	} else {
		s.sorted = make([]*Namespace, 0, len(s.namespaces))
	}

	for _, ns := range s.namespaces {
		s.sorted = append(s.sorted, ns)
	}

	sort.Slice(s.sorted, func(i, j int) bool {
		return s.sorted[i].order < s.sorted[j].order
	})
}

// ensureSorted ensures the namespace is sorted if dirty.
func (ns *Namespace) ensureSorted() {
	if ns.dirty {
		ns.sortComponents()
		ns.dirty = false
	}
}

// sortComponents sorts components by order.
func (ns *Namespace) sortComponents() {
	if cap(ns.sorted) >= len(ns.components) {
		ns.sorted = ns.sorted[:0]
	} else {
		ns.sorted = make([]*ComponentDescriptor, 0, len(ns.components))
	}

	for _, descriptor := range ns.components {
		ns.sorted = append(ns.sorted, descriptor)
	}

	sort.Slice(ns.sorted, func(i, j int) bool {
		return ns.sorted[i].Order < ns.sorted[j].Order
	})
}
