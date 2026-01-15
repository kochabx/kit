package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/kochabx/kit/log"
	httpmetrics "github.com/kochabx/kit/metrics/http"
	"github.com/kochabx/kit/transport"
)

var _ transport.Server = (*Server)(nil)

const (
	defaultName = "http"
	defaultAddr = ":8080"
)

// Meta is the metadata of the server.
type Meta struct {
	Name string
}

type Server struct {
	meta    Meta
	options Options
	server  *http.Server
}

type Option func(*Server)

func WithMeta(meta Meta) Option {
	return func(s *Server) {
		s.meta = meta
	}
}

func WithMetricsOptions(metrics MetricsOption) Option {
	return func(s *Server) {
		if err := metrics.init(); err != nil {
			log.Error().Err(err).Send()
			return
		}
		s.options.Metrics = metrics
	}
}

func WithSwagOptions(swag SwagOption) Option {
	return func(s *Server) {
		if err := swag.init(); err != nil {
			log.Error().Err(err).Send()
			return
		}
		s.options.Swag = swag
	}
}

func WithHealthOptions(health HealthOption) Option {
	return func(s *Server) {
		if err := health.init(); err != nil {
			log.Error().Err(err).Send()
			return
		}
		s.options.Health = health
	}
}

func NewServer(addr string, handler http.Handler, opts ...Option) *Server {
	s := &Server{
		server: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	additionalHandlers(s)

	return s
}

func (s *Server) Run() error {
	if s.meta.Name == "" {
		s.meta.Name = defaultName
	}

	if ok := transport.ValidateAddress(s.server.Addr); !ok {
		log.Warn().Msgf("invalid address %s, using default address: %s", s.server.Addr, defaultAddr)
		s.server.Addr = defaultAddr
	}
	log.Info().Msgf("%s server listening on %s", s.meta.Name, s.server.Addr)

	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func additionalHandlers(s *Server) {
	if r, ok := s.server.Handler.(*gin.Engine); ok {
		handleMetrics(s, r)
		handleSwag(s, r)
		handleHealth(s, r)
	}
}

func handleMetrics(s *Server, r *gin.Engine) {
	if s.options.Metrics.Enabled {
		if s.options.Metrics.EnabledGoCollector {
			httpmetrics.Prom.WithGoCollectorRuntimeMetrics()
		}
		if s.options.Metrics.EnabledBuildInfoCollector {
			httpmetrics.Prom.WithBuildInfoCollector()
		}

		r.GET(s.options.Metrics.Path, gin.WrapH(promhttp.HandlerFor(httpmetrics.Prom.Registry(), promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		})))
	}
}

func handleSwag(s *Server, r *gin.Engine) {
	if s.options.Swag.Enabled {
		r.GET(s.options.Swag.Path, ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
}

func handleHealth(s *Server, r *gin.Engine) {
	if s.options.Health.Enabled {
		r.GET(s.options.Health.Path, func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}
}
