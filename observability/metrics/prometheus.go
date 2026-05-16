package metrics

import "github.com/prometheus/client_golang/prometheus"

type Prometheus struct {
	registry *prometheus.Registry
}

var Prom = New()

func New(opts ...Option) *Prometheus {
	p := &Prometheus{
		registry: prometheus.NewRegistry(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Prometheus) Registry() *prometheus.Registry {
	return p.registry
}
