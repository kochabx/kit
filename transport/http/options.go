package http

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/observability/metrics"
)

type Options struct {
	Metrics *MetricsOption
	Swagger *SwaggerOption
	OpenAPI *OpenAPIOption
	Health  *HealthOption
}

// MetricsOption configures the Prometheus metrics endpoint.
type MetricsOption struct {
	Path     string `default:"/metrics"` // Endpoint path, defaults to "/metrics"
	Registry *prometheus.Registry
}

func (c *MetricsOption) init() error {
	if err := defaults.Apply(c); err != nil {
		return err
	}

	if c.Registry != nil {
		return nil
	}
	c.Registry = metrics.New(
		metrics.WithGoCollectorRuntimeMetrics(),
		metrics.WithBuildInfoCollector(),
	).Registry()
	return nil
}

// SwaggerOption configures the Swagger UI endpoint.
type SwaggerOption struct {
	Path string `default:"/swagger/"` // Endpoint path prefix, defaults to "/swagger/"
}

func (c *SwaggerOption) init() error {
	return defaults.Apply(c)
}

// OpenAPIOption configures a raw OpenAPI spec endpoint.
type OpenAPIOption struct {
	Path string `default:"/swagger/"` // Endpoint path prefix, defaults to "/swagger/"
	Spec []byte
}

func (c *OpenAPIOption) init() error {
	return defaults.Apply(c)
}

// HealthOption configures the health-check endpoint.
type HealthOption struct {
	Path string `default:"/health"` // Endpoint path, defaults to "/health"
}

func (c *HealthOption) init() error {
	return defaults.Apply(c)
}
