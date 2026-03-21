package metrics

import (
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var (
	Prom = New()
)

type Prometheus struct {
	registry *prometheus.Registry
}

func New() *Prometheus {
	p := &Prometheus{
		registry: prometheus.NewRegistry(),
	}

	return p
}

func (p *Prometheus) WithGoCollectorRuntimeMetrics() {
	p.registry.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")}),
	))
}

func (p *Prometheus) WithBuildInfoCollector() {
	p.registry.MustRegister(collectors.NewBuildInfoCollector())
}

func (p *Prometheus) Registry() *prometheus.Registry {
	return p.registry
}
