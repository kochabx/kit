package metrics

import (
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type Option func(*Prometheus)

func WithRegistry(registry *prometheus.Registry) Option {
	return func(p *Prometheus) {
		if registry != nil {
			p.registry = registry
		}
	}
}

func WithGoCollectorRuntimeMetrics() Option {
	return func(p *Prometheus) {
		p.registry.MustRegister(collectors.NewGoCollector(
			collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")}),
		))
	}
}

func WithBuildInfoCollector() Option {
	return func(p *Prometheus) {
		p.registry.MustRegister(collectors.NewBuildInfoCollector())
	}
}
