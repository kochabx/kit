package dig

import (
	"context"
	"fmt"
	"sort"
)

// injector resolves and wires Provider → Consumer dependencies within a Container.
type injector struct {
	c *Container

	// resolved dependency values keyed by the dependency name advertised via
	// Provider.ProvidesDependency().
	deps map[string]any
}

func newInjector(c *Container) *injector {
	return &injector{
		c:    c,
		deps: make(map[string]any),
	}
}

// Resolve discovers all providers, validates the dependency graph, then injects
// every Consumer in topological order. Must be called once before Start.
func (inj *injector) Resolve(ctx context.Context) error {
	all := inj.allComponents()

	// 1. Collect all providers.
	for _, comp := range all {
		if p, ok := comp.(Provider); ok {
			name := p.ProvidesDependency()
			if name != "" {
				inj.deps[name] = p.GetDependency()
			}
		}
	}

	// 2. Build graph and check for cycles.
	graph, err := inj.buildGraph(all)
	if err != nil {
		return err
	}

	if err = checkCycles(graph); err != nil {
		return err
	}

	// 3. Inject in topological order.
	ordered := topoSort(graph)
	for _, n := range ordered {
		if err = inj.injectInto(n.comp); err != nil {
			return err
		}
	}
	return nil
}

// allComponents returns every component registered across all groups.
func (inj *injector) allComponents() []Component {
	inj.c.mu.RLock()
	defer inj.c.mu.RUnlock()

	var all []Component
	for _, g := range inj.c.groups {
		for _, d := range g.components {
			all = append(all, d.Instance)
		}
	}
	return all
}

// injectInto satisfies all declared dependencies of a Consumer.
func (inj *injector) injectInto(comp Component) error {
	consumer, ok := comp.(Consumer)
	if !ok {
		return nil
	}
	for _, name := range consumer.RequiredDependencies() {
		dep, exists := inj.deps[name]
		if !exists {
			return fmt.Errorf("%w: %s (required by %s)", ErrDependencyNotFound, name, comp.Name())
		}
		if err := consumer.SetDependency(name, dep); err != nil {
			return fmt.Errorf("inject %s → %s: %w", name, comp.Name(), err)
		}
	}
	return nil
}

// --- Graph types -----------------------------------------------------------

type node struct {
	comp       Component
	deps       []*node // must be started before this node
	dependents []*node // must be started after this node
	visited    bool
	inStack    bool
}

func (inj *injector) buildGraph(all []Component) (map[string]*node, error) {
	// Index all components by name for fast lookup.
	byName := make(map[string]Component, len(all))
	for _, c := range all {
		byName[c.Name()] = c
	}

	// Index providers by the dependency name they advertise.
	providerOf := make(map[string]Component)
	for _, c := range all {
		if p, ok := c.(Provider); ok {
			depName := p.ProvidesDependency()
			if depName != "" {
				providerOf[depName] = c
			}
		}
	}

	graph := make(map[string]*node, len(all))
	getOrCreate := func(c Component) *node {
		n, ok := graph[c.Name()]
		if !ok {
			n = &node{comp: c}
			graph[c.Name()] = n
		}
		return n
	}

	// Only consumers have edges.
	for _, c := range all {
		consumer, ok := c.(Consumer)
		if !ok {
			continue
		}
		consumerNode := getOrCreate(c)
		for _, depName := range consumer.RequiredDependencies() {
			provider, exists := providerOf[depName]
			if !exists {
				return nil, fmt.Errorf("%w: %s (required by %s)", ErrDependencyNotFound, depName, c.Name())
			}
			providerNode := getOrCreate(provider)
			consumerNode.deps = append(consumerNode.deps, providerNode)
			providerNode.dependents = append(providerNode.dependents, consumerNode)
		}
	}

	return graph, nil
}

// checkCycles uses DFS with a call-stack marker to detect cycles.
func checkCycles(graph map[string]*node) error {
	var dfs func(n *node) error
	dfs = func(n *node) error {
		n.visited = true
		n.inStack = true
		for _, dep := range n.deps {
			if dep.inStack {
				return fmt.Errorf("%w: %s → %s", ErrCircularDependency, n.comp.Name(), dep.comp.Name())
			}
			if !dep.visited {
				if err := dfs(dep); err != nil {
					return err
				}
			}
		}
		n.inStack = false
		return nil
	}
	for _, n := range graph {
		if !n.visited {
			if err := dfs(n); err != nil {
				return err
			}
		}
	}
	return nil
}

// topoSort returns nodes ordered so that every dependency appears before its
// dependent (providers before consumers).
func topoSort(graph map[string]*node) []*node {
	// Reset visited flags for second pass.
	for _, n := range graph {
		n.visited = false
	}

	var stack []*node
	var dfs func(n *node)
	dfs = func(n *node) {
		n.visited = true
		for _, dep := range n.deps {
			if !dep.visited {
				dfs(dep)
			}
		}
		stack = append(stack, n)
	}
	// Iterate in stable order to get deterministic output.
	keys := make([]string, 0, len(graph))
	for k := range graph {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := graph[k]
		if !n.visited {
			dfs(n)
		}
	}

	// Reverse: providers first.
	result := make([]*node, len(stack))
	for i, n := range stack {
		result[len(stack)-1-i] = n
	}
	return result
}
