package http

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/kochabx/kit/log"
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

// Server manages the lifecycle of a framework-agnostic HTTP server.
type Server struct {
	srv  *http.Server
	opts options
}

// NewServer creates a server for handler
func NewServer(handler http.Handler, opts ...Option) *Server {
	o := options{
		addr:         defaultAddr,
		name:         defaultName,
		readTimeout:  defaultReadTimeout,
		writeTimeout: defaultWriteTimeout,
		idleTimeout:  defaultIdleTimeout,
	}
	for _, opt := range opts {
		opt(&o)
	}

	s := &Server{opts: o}

	s.srv = &http.Server{
		Addr:         o.addr,
		Handler:      withBuiltinEndpoints(handler, &o),
		ReadTimeout:  o.readTimeout,
		WriteTimeout: o.writeTimeout,
		IdleTimeout:  o.idleTimeout,
		TLSConfig:    o.tlsConfig,
	}

	return s
}

// Handler returns the effective handler, including configured built-in endpoints.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Start opens the listener and serves requests in the background.
func (s *Server) Start(_ context.Context) error {
	lis, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	if s.opts.tlsCertFile != "" {
		go s.srv.ServeTLS(lis, s.opts.tlsCertFile, s.opts.tlsKeyFile)
	} else {
		go s.srv.Serve(lis)
	}
	log.Info().Msgf("%s server listening on %s", s.opts.name, s.srv.Addr)
	return nil
}

// Stop gracefully shuts down the server using ctx.
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
