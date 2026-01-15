package ioc

import (
	"context"
	"fmt"
	"sort"
)

// SimpleDependencyInjector implements dependency injection without reflection.
type SimpleDependencyInjector struct {
	store        *Store
	dependencies map[string]any
	providers    map[string]DependencyProvider
}

// NewSimpleDependencyInjector creates a new dependency injector.
func NewSimpleDependencyInjector(store *Store) *SimpleDependencyInjector {
	return &SimpleDependencyInjector{
		store:        store,
		dependencies: make(map[string]any),
		providers:    make(map[string]DependencyProvider),
	}
}

// RegisterDependency registers a dependency provider for a specific component name.
func (sdi *SimpleDependencyInjector) RegisterDependency(name string, provider any) error {
	if name == "" {
		return fmt.Errorf("%w: empty component name", ErrInvalidComponent)
	}

	if _, exists := sdi.dependencies[name]; exists {
		return fmt.Errorf("%w: %s", ErrComponentAlreadyExists, name)
	}

	sdi.dependencies[name] = provider

	// If provider implements DependencyProvider interface, register it
	if dp, ok := provider.(DependencyProvider); ok {
		sdi.providers[name] = dp
	}

	return nil
}

// ResolveDependency resolves a dependency by its component name.
func (sdi *SimpleDependencyInjector) ResolveDependency(name string) (any, error) {
	if dependency, exists := sdi.dependencies[name]; exists {
		return dependency, nil
	}

	// Try to find a provider that can provide this dependency
	if provider, exists := sdi.providers[name]; exists {
		return provider.GetDependency(), nil
	}

	// Search all namespaces for a component that provides this dependency
	for _, namespace := range sdi.store.ListNamespaces() {
		components, _ := sdi.store.GetAllSafe(namespace)
		for _, component := range components {
			if provider, ok := component.(DependencyProvider); ok {
				if provider.ProvidesDependency() == name {
					dependency := provider.GetDependency()
					// Cache the dependency for future use
					sdi.dependencies[name] = dependency
					sdi.providers[name] = provider
					return dependency, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrDependencyNotFound, name)
}

// InjectInto injects dependencies into a component.
func (sdi *SimpleDependencyInjector) InjectInto(component Component) error {
	consumer, ok := component.(DependencyConsumer)
	if !ok {
		// Component doesn't require dependencies
		return nil
	}

	requiredDeps := consumer.RequiredDependencies()
	for _, name := range requiredDeps {
		dependency, err := sdi.ResolveDependency(name)
		if err != nil {
			return fmt.Errorf("failed to resolve dependency %s for component %s: %w",
				name, component.Name(), err)
		}

		if err := consumer.SetDependency(name, dependency); err != nil {
			return fmt.Errorf("failed to inject dependency %s into component %s: %w",
				name, component.Name(), err)
		}
	}

	return nil
}

// ResolveDependencies automatically resolves and injects dependencies for all registered components.
func (sdi *SimpleDependencyInjector) ResolveDependencies(ctx context.Context) error {
	// First, discover all dependency providers
	if err := sdi.discoverProviders(); err != nil {
		return fmt.Errorf("failed to discover providers: %w", err)
	}

	// Then, build dependency graph
	graph, err := sdi.buildDependencyGraph()
	if err != nil {
		return fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Check for circular dependencies
	if err := sdi.checkCircularDependencies(graph); err != nil {
		return err
	}

	// Inject dependencies in topological order
	return sdi.injectDependencies(ctx, graph)
}

// discoverProviders discovers all dependency providers in the container.
func (sdi *SimpleDependencyInjector) discoverProviders() error {
	for _, namespace := range sdi.store.ListNamespaces() {
		components, _ := sdi.store.GetAllSafe(namespace)
		for _, component := range components {
			if provider, ok := component.(DependencyProvider); ok {
				name := provider.ProvidesDependency()
				if name != "" {
					sdi.providers[name] = provider
					// Also cache the actual dependency
					sdi.dependencies[name] = provider.GetDependency()
				}
			}
		}
	}
	return nil
}

// DependencyNode represents a node in the dependency graph.
type DependencyNode struct {
	Component    Component
	Dependencies []*DependencyNode
	Dependents   []*DependencyNode
	Visited      bool
	InStack      bool
}

// buildDependencyGraph builds a dependency graph for components.
func (sdi *SimpleDependencyInjector) buildDependencyGraph() (map[string]*DependencyNode, error) {
	graph := make(map[string]*DependencyNode)

	// Create nodes for all components that require dependencies
	for _, namespace := range sdi.store.ListNamespaces() {
		components, _ := sdi.store.GetAllSafe(namespace)
		for _, component := range components {
			if consumer, ok := component.(DependencyConsumer); ok {
				graph[component.Name()] = &DependencyNode{
					Component:    component,
					Dependencies: make([]*DependencyNode, 0),
					Dependents:   make([]*DependencyNode, 0),
				}

				// Build dependency relationships
				requiredDeps := consumer.RequiredDependencies()
				for _, name := range requiredDeps {
					// Find provider for this dependency
					providerComponent, err := sdi.findProviderComponent(name)
					if err != nil {
						return nil, fmt.Errorf("failed to find provider for dependency %s required by %s: %w",
							name, component.Name(), err)
					}

					// Create or get provider node
					providerNode, exists := graph[providerComponent.Name()]
					if !exists {
						providerNode = &DependencyNode{
							Component:    providerComponent,
							Dependencies: make([]*DependencyNode, 0),
							Dependents:   make([]*DependencyNode, 0),
						}
						graph[providerComponent.Name()] = providerNode
					}

					// Add dependency relationship
					consumerNode := graph[component.Name()]
					consumerNode.Dependencies = append(consumerNode.Dependencies, providerNode)
					providerNode.Dependents = append(providerNode.Dependents, consumerNode)
				}
			}
		}
	}

	return graph, nil
}

// findProviderComponent finds a component that can provide the specified dependency name.
func (sdi *SimpleDependencyInjector) findProviderComponent(name string) (Component, error) {
	for _, namespace := range sdi.store.ListNamespaces() {
		components, _ := sdi.store.GetAllSafe(namespace)
		for _, component := range components {
			if provider, ok := component.(DependencyProvider); ok {
				if provider.ProvidesDependency() == name {
					return component, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrDependencyNotFound, name)
}

// checkCircularDependencies checks for circular dependencies in the graph using DFS.
func (sdi *SimpleDependencyInjector) checkCircularDependencies(graph map[string]*DependencyNode) error {
	for _, node := range graph {
		if !node.Visited {
			if err := sdi.dfsCircularCheck(node); err != nil {
				return err
			}
		}
	}
	return nil
}

// dfsCircularCheck performs depth-first search to detect circular dependencies.
func (sdi *SimpleDependencyInjector) dfsCircularCheck(node *DependencyNode) error {
	node.Visited = true
	node.InStack = true

	for _, dep := range node.Dependencies {
		if dep.InStack {
			return fmt.Errorf("%w: %s -> %s", ErrCircularDependency, node.Component.Name(), dep.Component.Name())
		}

		if !dep.Visited {
			if err := sdi.dfsCircularCheck(dep); err != nil {
				return err
			}
		}
	}

	node.InStack = false
	return nil
}

// injectDependencies injects dependencies in topological order.
func (sdi *SimpleDependencyInjector) injectDependencies(ctx context.Context, graph map[string]*DependencyNode) error {
	// Get topological order
	order, err := sdi.topologicalSort(graph)
	if err != nil {
		return err
	}

	// Inject dependencies in order
	for _, node := range order {
		if err := sdi.InjectInto(node.Component); err != nil {
			return err
		}
	}

	return nil
}

// topologicalSort performs topological sorting of the dependency graph.
func (sdi *SimpleDependencyInjector) topologicalSort(graph map[string]*DependencyNode) ([]*DependencyNode, error) {
	// Reset visited flags
	for _, node := range graph {
		node.Visited = false
	}

	var result []*DependencyNode
	var stack []*DependencyNode

	// Perform DFS for topological sort
	for _, node := range graph {
		if !node.Visited {
			if err := sdi.dfsTopological(node, &stack); err != nil {
				return nil, err
			}
		}
	}

	// Reverse the stack to get correct topological order
	for i := len(stack) - 1; i >= 0; i-- {
		result = append(result, stack[i])
	}

	return result, nil
}

// dfsTopological performs DFS for topological sorting.
func (sdi *SimpleDependencyInjector) dfsTopological(node *DependencyNode, stack *[]*DependencyNode) error {
	node.Visited = true

	for _, dep := range node.Dependencies {
		if !dep.Visited {
			if err := sdi.dfsTopological(dep, stack); err != nil {
				return err
			}
		}
	}

	*stack = append(*stack, node)
	return nil
}

// ValidateComponent validates a component before registration.
func ValidateComponent(component Component) error {
	if component == nil {
		return fmt.Errorf("%w: component is nil", ErrInvalidComponent)
	}

	if component.Name() == "" {
		return fmt.Errorf("%w: component name is empty", ErrInvalidComponent)
	}

	return nil
}

// GetDependencyGraph returns a visualization of the dependency graph.
func (sdi *SimpleDependencyInjector) GetDependencyGraph() map[string][]string {
	graph := make(map[string][]string)

	for _, namespace := range sdi.store.ListNamespaces() {
		components, _ := sdi.store.GetAllSafe(namespace)
		for _, component := range components {
			if consumer, ok := component.(DependencyConsumer); ok {
				componentName := component.Name()
				requiredDeps := consumer.RequiredDependencies()

				var dependencies []string
				for _, name := range requiredDeps {
					providerComponent, err := sdi.findProviderComponent(name)
					if err == nil {
						dependencies = append(dependencies, providerComponent.Name())
					} else {
						dependencies = append(dependencies, fmt.Sprintf("MISSING: %s", name))
					}
				}

				sort.Strings(dependencies)
				graph[componentName] = dependencies
			}
		}
	}

	return graph
}
