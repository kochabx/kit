package cx

type ContainerMetrics struct {
	ComponentCount int
	State          State
}

func (c *Container) Metrics() ContainerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ContainerMetrics{
		ComponentCount: len(c.providers),
		State:          c.state,
	}
}
