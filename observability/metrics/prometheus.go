package metrics

import "github.com/prometheus/client_golang/prometheus"

type Prometheus struct {
	registry   *prometheus.Registry
	collectors []prometheus.Collector
}

var Prom = New()

func New(opts ...Option) *Prometheus {
	p := &Prometheus{
		registry: prometheus.NewRegistry(),
	}
	for _, opt := range opts {
		opt(p)
	}
	for _, collector := range p.collectors {
		p.registry.MustRegister(collector)
	}
	return p
}

func (p *Prometheus) Registry() *prometheus.Registry {
	return p.registry
}
