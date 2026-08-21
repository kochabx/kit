package grpc

import (
	"context"
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
	lis  net.Listener
}

// NewServer creates a gRPC server with the provided options.
// Register service implementations on Srv() before calling Run.
func NewServer(opts ...Option) *Server {
	cfg := options{
		addr: defaultAddr,
		name: defaultName,
	}
	for _, opt := range opts {
		opt(&cfg)
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

// Start implements cx.Starter, starting the gRPC server in the background and returning immediately.
func (s *Server) Start(_ context.Context) error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.lis = lis
	go s.srv.Serve(lis)
	log.Info().Msgf("%s server listening on %s", s.name, s.addr)
	return nil
}

// Stop gracefully stops the gRPC server and waits for the background goroutine to exit.
func (s *Server) Stop(ctx context.Context) error {
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
