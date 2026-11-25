package cx

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// build resolves name recursively using an immutable local dependency stack so
// concurrent Get calls inside constructors never corrupt shared container state.
func (c *Container) build(name string, stack []string) error {
	c.mu.Lock()
	round := c.round
	if round == nil || !round.open {
		c.mu.Unlock()
		return fmt.Errorf("%w: container build closed", ErrInvalidState)
	}
	round.wg.Add(1)
	defer round.wg.Done()
	gen := c.buildGen

	p, exists := c.providers[name]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrComponentNotFound, name)
	}
	if p.built {
		c.recordDependencyLocked(name, stack)
		c.mu.Unlock()
		return nil
	}
	if idx := slices.Index(stack, name); idx >= 0 {
		cycle := append(slices.Clone(stack[idx:]), name)
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrCircularDependency, strings.Join(cycle, " -> "))
	}

	c.recordDependencyLocked(name, stack)
	waiter := ""
	if len(stack) > 0 {
		waiter = stack[len(stack)-1]
		if cycle := round.findWaitCycleLocked(name, waiter); len(cycle) > 0 {
			c.mu.Unlock()
			return fmt.Errorf("%w: %s", ErrCircularDependency, strings.Join(cycle, " -> "))
		}
		round.addWaitEdgeLocked(waiter, name)
	}
	if flight := round.flights[name]; flight != nil {
		c.mu.Unlock()
		<-flight.done
		if waiter != "" {
			c.mu.Lock()
			round.removeWaitEdgeLocked(waiter, name)
			c.mu.Unlock()
		}
		return flight.err
	}
	flight := &buildFlight{done: make(chan struct{})}
	round.flights[name] = flight
	nextStack := append(slices.Clone(stack), name)
	declaredDeps := slices.Clone(p.declaredDeps)
	constructor := p.constructor
	c.mu.Unlock()

	var buildErr error
	defer func() {
		c.mu.Lock()
		if waiter != "" {
			round.removeWaitEdgeLocked(waiter, name)
		}
		if round.flights[name] == flight {
			delete(round.flights, name)
		}
		flight.err = buildErr
		close(flight.done)
		c.mu.Unlock()
	}()

	for _, dep := range declaredDeps {
		if err := c.build(dep, nextStack); err != nil {
			buildErr = fmt.Errorf("dependency %s of %s: %w", dep, name, err)
			return buildErr
		}
	}

	scoped := &Container{containerCore: c.containerCore, stack: nextStack}
	value, err := invokeConstructor(scoped, name, constructor)
	if err != nil {
		buildErr = fmt.Errorf("construct %s: %w", name, err)
		return buildErr
	}

	c.mu.Lock()
	if c.buildGen != gen {
		stopTimeout := c.componentStopTimeout
		c.mu.Unlock()
		if stopper, ok := value.(Stopper); ok {
			stopCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
			_ = stopper.Stop(stopCtx)
			cancel()
		}
		buildErr = fmt.Errorf("%w: container build aborted", ErrInvalidState)
		return buildErr
	}
	if !p.built {
		p.value = value
		p.built = true
		_, p.owned = value.(Stopper)
		c.buildOrder = append(c.buildOrder, name)
	}
	c.mu.Unlock()
	return nil
}

func invokeConstructor(
	sc *Container,
	name string,
	constructor func(*Container) (any, error),
) (value any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %s: %v", ErrConstructorPanic, name, recovered)
		}
	}()
	return constructor(sc)
}

func (c *Container) recordDependencyLocked(name string, stack []string) {
	if len(stack) == 0 {
		return
	}
	caller := c.providers[stack[len(stack)-1]]
	if caller != nil && !slices.Contains(caller.deps, name) {
		caller.deps = append(caller.deps, name)
	}
}

func (r *buildRound) addWaitEdgeLocked(waiter, target string) {
	edges := r.waiting[waiter]
	if edges == nil {
		edges = make(map[string]int)
		r.waiting[waiter] = edges
	}
	edges[target]++
}

func (r *buildRound) removeWaitEdgeLocked(waiter, target string) {
	edges := r.waiting[waiter]
	if edges == nil {
		return
	}
	if count := edges[target]; count <= 1 {
		delete(edges, target)
		if len(edges) == 0 {
			delete(r.waiting, waiter)
		}
	} else {
		edges[target] = count - 1
	}
}

func (r *buildRound) findWaitCycleLocked(start, target string) []string {
	visited := map[string]bool{start: true}
	var dfs func(cur string) []string
	dfs = func(cur string) []string {
		if cur == target {
			return []string{target}
		}
		for next := range r.waiting[cur] {
			if visited[next] {
				continue
			}
			visited[next] = true
			if tail := dfs(next); len(tail) > 0 {
				return append([]string{cur}, tail...)
			}
		}
		return nil
	}
	if tail := dfs(start); len(tail) > 0 {
		return append([]string{target}, tail...)
	}
	return nil
}
