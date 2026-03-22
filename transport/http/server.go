package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/swaggo/swag"

	"github.com/kochabx/kit/log"
	httpmetrics "github.com/kochabx/kit/observability/metrics/http"
	"github.com/kochabx/kit/transport"
)

var _ transport.Server = (*Server)(nil)

const (
	defaultName         = "http"
	defaultAddr         = ":8080"
	defaultReadTimeout  = 10 * time.Second
	defaultWriteTimeout = 30 * time.Second
	defaultIdleTimeout  = 60 * time.Second
)

// Server is a framework-agnostic HTTP server. Any http.Handler implementation
// (gin, chi, echo, stdlib mux, etc.) can be passed to NewServer.
type Server struct {
	srv         *http.Server
	name        string
	tlsCertFile string
	tlsKeyFile  string
}

// config holds the builder state for NewServer.
type config struct {
	addr         string
	name         string
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	tlsCertFile  string
	tlsKeyFile   string
	tlsConfig    *tls.Config
	options      Options
}

// Option configures a Server.
type Option func(*config)

func defaultConfig() config {
	return config{
		addr:         defaultAddr,
		name:         defaultName,
		readTimeout:  defaultReadTimeout,
		writeTimeout: defaultWriteTimeout,
		idleTimeout:  defaultIdleTimeout,
	}
}

// WithAddr sets the TCP address the server listens on (e.g. ":8080").
func WithAddr(addr string) Option {
	return func(c *config) { c.addr = addr }
}

// WithName sets the server name, used in log output.
func WithName(name string) Option {
	return func(c *config) { c.name = name }
}

// WithTimeout overrides the HTTP server read, write, and idle timeouts.
func WithTimeout(read, write, idle time.Duration) Option {
	return func(c *config) {
		c.readTimeout = read
		c.writeTimeout = write
		c.idleTimeout = idle
	}
}

// WithTLS enables TLS using the provided certificate and private key files.
// When set, Run() uses ListenAndServeTLS instead of ListenAndServe.
func WithTLS(certFile, keyFile string) Option {
	return func(c *config) {
		c.tlsCertFile = certFile
		c.tlsKeyFile = keyFile
	}
}

// WithTLSConfig sets a custom *tls.Config on the underlying http.Server.
func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(c *config) { c.tlsConfig = tlsCfg }
}

// WithMetrics enables the Prometheus metrics endpoint.
func WithMetrics(cfg MetricsOption) Option {
	return func(c *config) {
		if err := cfg.init(); err != nil {
			log.Error().Err(err).Msg("WithMetrics: init defaults failed")
			return
		}
		c.options.Metrics = &cfg
	}
}

// WithSwagger enables the Swagger UI endpoint.
func WithSwagger(cfg SwaggerOption) Option {
	return func(c *config) {
		if err := cfg.init(); err != nil {
			log.Error().Err(err).Msg("WithSwagger: init defaults failed")
			return
		}
		c.options.Swagger = &cfg
	}
}

// WithOpenAPI enables a raw OpenAPI spec endpoint.
func WithOpenAPI(cfg OpenAPIOption) Option {
	return func(c *config) {
		if err := cfg.init(); err != nil {
			log.Error().Err(err).Msg("WithOpenAPI: init defaults failed")
			return
		}
		c.options.OpenAPI = &cfg
	}
}

// WithHealth enables the health-check endpoint.
func WithHealth(cfg HealthOption) Option {
	return func(c *config) {
		if err := cfg.init(); err != nil {
			log.Error().Err(err).Msg("WithHealth: init defaults failed")
			return
		}
		c.options.Health = &cfg
	}
}

// NewServer creates a framework-agnostic HTTP server wrapping any http.Handler.
// Pass a *gin.Engine, chi.Mux, echo.Echo, http.ServeMux, or any other handler.
// Built-in endpoints (health, metrics, swagger) are served by a net/http.ServeMux
// layered on top — independent of the user's web framework.
func NewServer(handler http.Handler, opts ...Option) *Server {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	if !transport.ValidateAddress(cfg.addr) {
		log.Warn().Msgf("invalid address %q, falling back to %s", cfg.addr, defaultAddr)
		cfg.addr = defaultAddr
	}

	s := &Server{
		name:        cfg.name,
		tlsCertFile: cfg.tlsCertFile,
		tlsKeyFile:  cfg.tlsKeyFile,
	}

	s.srv = &http.Server{
		Addr:         cfg.addr,
		Handler:      buildHandler(handler, &cfg),
		ReadTimeout:  cfg.readTimeout,
		WriteTimeout: cfg.writeTimeout,
		IdleTimeout:  cfg.idleTimeout,
		TLSConfig:    cfg.tlsConfig,
	}

	return s
}

// Handler returns the http.Handler used by the underlying http.Server.
// When built-in endpoints are configured this is a net/http.ServeMux that
// wraps the user handler; otherwise the user handler is returned as-is.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Run starts the server. If TLS cert/key files were configured via WithTLS,
// the server uses HTTPS. Returns nil on clean shutdown (ErrServerClosed).
func (s *Server) Run() error {
	log.Info().Msgf("%s server listening on %s", s.name, s.srv.Addr)
	var err error
	if s.tlsCertFile != "" {
		err = s.srv.ListenAndServeTLS(s.tlsCertFile, s.tlsKeyFile)
	} else {
		err = s.srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops the server with the given context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// buildHandler wraps userHandler with a net/http.ServeMux for built-in endpoints.
// If no built-in endpoints are configured, userHandler is returned unchanged.
func buildHandler(userHandler http.Handler, cfg *config) http.Handler {
	opts := cfg.options
	if opts.Metrics == nil && opts.Swagger == nil && opts.OpenAPI == nil && opts.Health == nil {
		return userHandler
	}

	mux := http.NewServeMux()

	if opts.Health != nil {
		mux.HandleFunc(opts.Health.Path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
	}

	if opts.Metrics != nil {
		if opts.Metrics.EnableGoCollector {
			httpmetrics.Prom.WithGoCollectorRuntimeMetrics()
		}
		if opts.Metrics.EnableBuildInfo {
			httpmetrics.Prom.WithBuildInfoCollector()
		}
		mux.Handle(opts.Metrics.Path, promhttp.HandlerFor(httpmetrics.Prom.Registry(), promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}))
	}

	if opts.Swagger != nil {
		mux.Handle(prefixPath(opts.Swagger.Path), httpSwagger.WrapHandler)
	}

	if opts.OpenAPI != nil {
		if len(opts.OpenAPI.Spec) == 0 {
			log.Error().Msg("openapi: Spec is empty, skipping registration")
		} else {
			const instanceName = "openapi"
			swag.Register(instanceName, openapiSpec(opts.OpenAPI.Spec))
			mux.Handle(prefixPath(opts.OpenAPI.Path), httpSwagger.Handler(httpSwagger.InstanceName(instanceName)))
		}
	}

	// All unmatched requests fall through to the user's handler.
	mux.Handle("/", userHandler)
	return mux
}

// prefixPath normalizes a path for net/http.ServeMux prefix matching.
// Strips gin-style wildcard suffixes (e.g. /*any) and ensures a trailing slash.
func prefixPath(path string) string {
	if idx := strings.Index(path, "/*"); idx != -1 {
		path = path[:idx]
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

type openapiSpec string

func (o openapiSpec) ReadDoc() string { return string(o) }
