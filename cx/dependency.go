package cx

import (
	"fmt"
	"slices"
	"strings"
)

func (c *Container) build(name string) error {
	c.mu.Lock()
	p, exists := c.providers[name]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrComponentNotFound, name)
	}
	if p.built {
		c.recordDependencyLocked(name)
		c.mu.Unlock()
		return nil
	}
	for index, current := range c.buildStack {
		if current == name {
			cycle := append(slices.Clone(c.buildStack[index:]), name)
			c.mu.Unlock()
			return fmt.Errorf("%w: %s", ErrCircularDependency, strings.Join(cycle, " -> "))
		}
	}

	c.recordDependencyLocked(name)
	c.buildStack = append(c.buildStack, name)
	declaredDependencies := slices.Clone(p.declaredDeps)
	constructor := p.constructor
	c.mu.Unlock()

	for _, dependency := range declaredDependencies {
		if err := c.build(dependency); err != nil {
			c.popBuildStack(name)
			return fmt.Errorf("dependency %s of %s: %w", dependency, name, err)
		}
	}

	value, err := invokeConstructor(c, name, constructor)
	c.mu.Lock()
	c.popBuildStackLocked(name)
	if err == nil {
		p.value = value
		p.built = true
		_, p.owned = value.(Stopper)
		c.buildOrder = append(c.buildOrder, name)
	}
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("construct %s: %w", name, err)
	}
	return nil
}

func invokeConstructor(
	c *Container,
	name string,
	constructor func(*Container) (any, error),
) (value any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %s: %v", ErrConstructorPanic, name, recovered)
		}
	}()
	return constructor(c)
}

func (c *Container) recordDependencyLocked(name string) {
	if len(c.buildStack) == 0 {
		return
	}
	caller := c.providers[c.buildStack[len(c.buildStack)-1]]
	if !slices.Contains(caller.deps, name) {
		caller.deps = append(caller.deps, name)
	}
}

func (c *Container) popBuildStack(name string) {
	c.mu.Lock()
	c.popBuildStackLocked(name)
	c.mu.Unlock()
}

func (c *Container) popBuildStackLocked(name string) {
	last := len(c.buildStack) - 1
	if last >= 0 && c.buildStack[last] == name {
		c.buildStack = c.buildStack[:last]
	}
}
