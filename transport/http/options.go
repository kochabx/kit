package http

import (
	"crypto/tls"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/observability/metrics"
)

// options contains the settings applied while constructing a Server.
type options struct {
	addr         string
	name         string
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	tlsCertFile  string
	tlsKeyFile   string
	tlsConfig    *tls.Config
	metrics      *MetricsOption
	swagger      *SwaggerOption
	openAPI      *OpenAPIOption
	health       *HealthOption
}

// Option configures a Server.
type Option func(*options)

// WithAddr sets the TCP address the server listens on (e.g. ":8080").
func WithAddr(addr string) Option { return func(o *options) { o.addr = addr } }

// WithName sets the server name, used in log output.
func WithName(name string) Option { return func(o *options) { o.name = name } }

// WithTimeout overrides the HTTP server read, write, and idle timeouts.
func WithTimeout(read, write, idle time.Duration) Option {
	return func(o *options) {
		o.readTimeout = read
		o.writeTimeout = write
		o.idleTimeout = idle
	}
}

// WithTLS enables TLS using the provided certificate and private key files.
// When configured, Start serves the listener with TLS.
func WithTLS(certFile, keyFile string) Option {
	return func(o *options) {
		o.tlsCertFile = certFile
		o.tlsKeyFile = keyFile
	}
}

// WithTLSConfig sets a custom *tls.Config on the underlying http.Server.
func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(o *options) { o.tlsConfig = tlsCfg }
}

// WithMetrics enables the Prometheus metrics endpoint.
func WithMetrics(opt MetricsOption) Option {
	return func(o *options) {
		if err := opt.init(); err != nil {
			log.Error().Err(err).Msg("WithMetrics: init defaults failed")
			return
		}
		o.metrics = &opt
	}
}

// WithSwagger enables the Swagger UI endpoint.
func WithSwagger(opt SwaggerOption) Option {
	return func(o *options) {
		if err := defaults.Apply(&opt); err != nil {
			log.Error().Err(err).Msg("WithSwagger: init defaults failed")
			return
		}
		o.swagger = &opt
	}
}

// WithOpenAPI enables a raw OpenAPI spec endpoint.
func WithOpenAPI(opt OpenAPIOption) Option {
	return func(o *options) {
		if err := defaults.Apply(&opt); err != nil {
			log.Error().Err(err).Msg("WithOpenAPI: init defaults failed")
			return
		}
		o.openAPI = &opt
	}
}

// WithHealth enables the health-check endpoint.
func WithHealth(opt HealthOption) Option {
	return func(o *options) {
		if err := defaults.Apply(&opt); err != nil {
			log.Error().Err(err).Msg("WithHealth: init defaults failed")
			return
		}
		o.health = &opt
	}
}

// MetricsOption configures the Prometheus metrics endpoint.
type MetricsOption struct {
	Path     string `default:"/metrics"` // Path defaults to "/metrics".
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
	Path string `default:"/swagger/"` // Path prefix defaults to "/swagger/".
}

// OpenAPIOption configures a raw OpenAPI spec endpoint.
type OpenAPIOption struct {
	Path string `default:"/openapi/"` // Path defaults to "/openapi/".
	Spec []byte
}

// HealthOption configures the health-check endpoint.
type HealthOption struct {
	Path string `default:"/health"` // Path defaults to "/health".
}
