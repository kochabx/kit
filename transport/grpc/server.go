package grpc

import (
	"context"
	"crypto/tls"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/transport"
)

var _ transport.Server = (*Server)(nil)

const (
	defaultName = "grpc"
	defaultAddr = ":50051"
)

// Server is the gRPC server wrapper.
type Server struct {
	srv  *grpc.Server
	addr string
	name string
}

// config holds the builder state for NewServer.
type config struct {
	addr               string
	name               string
	tlsConfig          *tls.Config
	unaryInterceptors  []grpc.UnaryServerInterceptor
	streamInterceptors []grpc.StreamServerInterceptor
	serverOptions      []grpc.ServerOption
}

// Option configures a Server.
type Option func(*config)

func defaultConfig() config {
	return config{
		addr: defaultAddr,
		name: defaultName,
	}
}

// WithAddr sets the TCP address the server listens on (e.g. ":50051").
func WithAddr(addr string) Option {
	return func(c *config) { c.addr = addr }
}

// WithName sets the server name, used in log output.
func WithName(name string) Option {
	return func(c *config) { c.name = name }
}

// WithTLSConfig enables TLS using the provided *tls.Config.
func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(c *config) { c.tlsConfig = tlsCfg }
}

// WithUnaryInterceptor appends unary server interceptors applied in order.
func WithUnaryInterceptor(interceptors ...grpc.UnaryServerInterceptor) Option {
	return func(c *config) {
		c.unaryInterceptors = append(c.unaryInterceptors, interceptors...)
	}
}

// WithStreamInterceptor appends stream server interceptors applied in order.
func WithStreamInterceptor(interceptors ...grpc.StreamServerInterceptor) Option {
	return func(c *config) {
		c.streamInterceptors = append(c.streamInterceptors, interceptors...)
	}
}

// WithServerOption appends raw grpc.ServerOption values.
func WithServerOption(opts ...grpc.ServerOption) Option {
	return func(c *config) {
		c.serverOptions = append(c.serverOptions, opts...)
	}
}

// NewServer creates a gRPC server with the provided options.
// Register service implementations on Srv() before calling Run.
func NewServer(opts ...Option) *Server {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	if !transport.ValidateAddress(cfg.addr) {
		log.Warn().Msgf("invalid address %q, falling back to %s", cfg.addr, defaultAddr)
		cfg.addr = defaultAddr
	}

	serverOpts := cfg.serverOptions
	if len(cfg.unaryInterceptors) > 0 {
		serverOpts = append(serverOpts, grpc.ChainUnaryInterceptor(cfg.unaryInterceptors...))
	}
	if len(cfg.streamInterceptors) > 0 {
		serverOpts = append(serverOpts, grpc.ChainStreamInterceptor(cfg.streamInterceptors...))
	}
	if cfg.tlsConfig != nil {
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(cfg.tlsConfig)))
	}

	return &Server{
		srv:  grpc.NewServer(serverOpts...),
		addr: cfg.addr,
		name: cfg.name,
	}
}

// Srv returns the underlying *grpc.Server for service registration.
func (s *Server) Srv() *grpc.Server { return s.srv }

// Run starts the gRPC server. It blocks until the server is stopped.
func (s *Server) Run() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	log.Info().Msgf("%s server listening on %s", s.name, s.addr)
	return s.srv.Serve(lis)
}

// Shutdown gracefully stops the server, waiting for in-flight RPCs to finish.
// If ctx is cancelled before all RPCs complete, the server is forcefully stopped.
func (s *Server) Shutdown(ctx context.Context) error {
	stopped := make(chan struct{})
	go func() {
		s.srv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-ctx.Done():
		s.srv.Stop()
	case <-stopped:
	}
	return nil
}
