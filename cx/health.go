package cx

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

type HealthStatus uint8

const (
	HealthHealthy HealthStatus = iota
	HealthUnhealthy
	HealthSkipped
	HealthTimeout
)

func (s HealthStatus) String() string {
	names := [...]string{"healthy", "unhealthy", "skipped", "timeout"}
	if int(s) >= len(names) {
		return "unknown"
	}
	return names[s]
}

type ComponentHealth struct {
	Key    string
	Status HealthStatus
	Error  error
}

type HealthReport struct {
	Components []ComponentHealth
	Healthy    bool
}

type healthFlight struct {
	done chan struct{}
	err  error
}

func (c *Container) HealthCheck(ctx context.Context) (HealthReport, error) {
	if ctx == nil {
		return HealthReport{}, ErrNilContext
	}
	c.mu.RLock()
	if c.state != StateRunning {
		state := c.state
		c.mu.RUnlock()
		return HealthReport{}, fmt.Errorf("%w: cannot health check in %s", ErrInvalidState, state)
	}
	order := slices.Clone(c.buildOrder)
	values := make([]any, len(order))
	for index, name := range order {
		values[index] = c.providers[name].value
	}
	c.mu.RUnlock()

	results := make([]ComponentHealth, len(order))
	var wg sync.WaitGroup
	for index, name := range order {
		checker, ok := values[index].(HealthChecker)
		if !ok {
			results[index] = ComponentHealth{Key: name, Status: HealthSkipped}
			continue
		}
		wg.Go(func() {
			results[index] = c.checkComponentHealth(ctx, name, checker)
		})
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return HealthReport{}, err
	}
	report := HealthReport{Components: results, Healthy: true}
	for _, result := range results {
		if result.Status == HealthUnhealthy || result.Status == HealthTimeout {
			report.Healthy = false
		}
	}
	return report, nil
}

func (c *Container) checkComponentHealth(
	ctx context.Context,
	name string,
	checker HealthChecker,
) ComponentHealth {
	flight := c.healthFlight(ctx, name, checker)
	select {
	case <-flight.done:
		return componentHealthResult(name, flight.err)
	case <-ctx.Done():
		return componentHealthResult(name, ctx.Err())
	}
}

func componentHealthResult(name string, err error) ComponentHealth {
	if err == nil {
		return ComponentHealth{Key: name, Status: HealthHealthy}
	}
	status := HealthUnhealthy
	if errors.Is(err, context.DeadlineExceeded) {
		status = HealthTimeout
	}
	return ComponentHealth{Key: name, Status: status, Error: err}
}

func (c *Container) healthFlight(
	ctx context.Context,
	name string,
	checker HealthChecker,
) *healthFlight {
	c.mu.Lock()
	if flight := c.healthFlights[name]; flight != nil {
		c.mu.Unlock()
		return flight
	}
	flight := &healthFlight{done: make(chan struct{})}
	c.healthFlights[name] = flight
	timeout := c.healthTimeout
	c.mu.Unlock()

	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	go func() {
		checked := make(chan error, 1)
		go func() {
			checked <- checker.HealthCheck(checkCtx)
		}()

		var err error
		timedOut := false
		select {
		case err = <-checked:
			cancel()
		case <-checkCtx.Done():
			err = checkCtx.Err()
			timedOut = true
			cancel()
		}

		c.mu.Lock()
		flight.err = err
		close(flight.done)
		if !timedOut {
			if c.healthFlights[name] == flight {
				delete(c.healthFlights, name)
			}
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		// Keep the timed-out flight slot registered while the stuck probe runs to cap
		// concurrency at 1, then remove it once HealthCheck actually finishes so future probes recover.
		<-checked
		c.mu.Lock()
		if c.healthFlights[name] == flight {
			delete(c.healthFlights, name)
		}
		c.mu.Unlock()
	}()
	return flight
}
