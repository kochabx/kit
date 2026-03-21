package http

import "github.com/kochabx/kit/core/defaults"

type Options struct {
	Metrics *MetricsOption
	Swagger *SwaggerOption
	OpenAPI *OpenAPIOption
	Health  *HealthOption
}

// MetricsOption configures the Prometheus metrics endpoint.
type MetricsOption struct {
	Path              string `default:"/metrics"` // Endpoint path, defaults to "/metrics"
	EnableGoCollector bool
	EnableBuildInfo   bool
}

func (c *MetricsOption) init() error {
	return defaults.Apply(c)
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
