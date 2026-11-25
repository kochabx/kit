package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/kochabx/kit/cx"
)

type serverOptions struct {
	addr            string
	name            string
	readTimeout     time.Duration
	writeTimeout    time.Duration
	idleTimeout     time.Duration
	tlsConfig       *tls.Config
	tlsCertFile     string
	tlsKeyFile      string
	tlsCertFilesSet bool
	protocols       *http.Protocols
	logger          *slog.Logger
	otelService     string
	healthContainer *cx.Container
	healthPath      string
	openAPI         *OpenAPIConfig
}

// ServerOption configures Server construction.
type ServerOption func(*serverOptions)

func WithAddr(addr string) ServerOption {
	return func(o *serverOptions) {
		if addr != "" {
			o.addr = addr
		}
	}
}

func WithName(name string) ServerOption {
	return func(o *serverOptions) {
		if name != "" {
			o.name = name
		}
	}
}

func WithTimeouts(read, write, idle time.Duration) ServerOption {
	return func(o *serverOptions) {
		if read > 0 {
			o.readTimeout = read
		}
		if write > 0 {
			o.writeTimeout = write
		}
		if idle > 0 {
			o.idleTimeout = idle
		}
	}
}

// WithTLSConfig clones tlsCfg so callers cannot mutate server TLS state later.
func WithTLSConfig(tlsCfg *tls.Config) ServerOption {
	return func(o *serverOptions) {
		if tlsCfg != nil {
			o.tlsConfig = tlsCfg.Clone()
		}
	}
}

func WithTLSCertFiles(certFile, keyFile string) ServerOption {
	return func(o *serverOptions) {
		o.tlsCertFilesSet = true
		o.tlsCertFile = certFile
		o.tlsKeyFile = keyFile
	}
}

func WithProtocols(p *http.Protocols) ServerOption {
	return func(o *serverOptions) {
		o.protocols = p
	}
}

func WithLogger(logger *slog.Logger) ServerOption {
	return func(o *serverOptions) {
		if logger != nil {
			o.logger = logger
		}
	}
}

func WithOTel(serviceName string) ServerOption {
	return func(o *serverOptions) { o.otelService = serviceName }
}

func WithHealthEndpoint(path string, c *cx.Container) ServerOption {
	return func(o *serverOptions) {
		if path != "" {
			o.healthPath = path
		}
		o.healthContainer = c
	}
}

func WithOpenAPI(cfg OpenAPIConfig) ServerOption {
	return func(o *serverOptions) { o.openAPI = &cfg }
}

type serverState uint8

const (
	serverIdle serverState = iota
	serverRunning
	serverDraining
	serverFailed
	serverClosed
)

// Server manages net/http lifecycle, TLS/h2c protocols, OTel wrapping, and built-in endpoints.
type Server struct {
	srv      *http.Server
	opts     serverOptions
	mu       sync.Mutex
	addr     string
	state    serverState
	serveErr error
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewServer constructs an HTTP Server around handler (no listener opened until Start).
func NewServer(handler http.Handler, opts ...ServerOption) *Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	o := serverOptions{
		addr:         ":8080",
		name:         "http",
		readTimeout:  10 * time.Second,
		writeTimeout: 30 * time.Second,
		idleTimeout:  60 * time.Second,
		logger:       slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	effective := mountBuiltins(handler, &o)
	if o.otelService != "" {
		effective = otelhttp.NewHandler(effective, o.otelService)
	}

	return &Server{
		opts: o,
		srv: &http.Server{
			Addr:         o.addr,
			Handler:      effective,
			ReadTimeout:  o.readTimeout,
			WriteTimeout: o.writeTimeout,
			IdleTimeout:  o.idleTimeout,
			TLSConfig:    o.tlsConfig,
			Protocols:    o.protocols,
		},
	}
}

func mountBuiltins(handler http.Handler, o *serverOptions) http.Handler {
	if o.healthPath == "" && o.openAPI == nil {
		return handler
	}
	mux := http.NewServeMux()
	if o.healthPath != "" {
		mux.HandleFunc("GET "+o.healthPath, HealthHandler(o.healthContainer))
	}
	if o.openAPI != nil {
		mountOpenAPI(mux, *o.openAPI)
	}
	mux.Handle("/", handler)
	return mux
}

// Handler returns the root http.Handler (including built-in endpoints and OTel).
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Addr returns the bound listener address after Start, or the configured Addr before Start.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.addr != "" {
		return s.addr
	}
	return s.srv.Addr
}

// Start opens the TCP listener and begins serving HTTP or HTTPS requests.
// If TLSConfig is provided without file paths, ServeTLS is still used so TLS never downgrades silently.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	switch s.state {
	case serverClosed:
		s.mu.Unlock()
		return errors.New("http: server already stopped")
	case serverIdle:
	default:
		s.mu.Unlock()
		return errors.New("http: server already started")
	}

	tlsCfg, err := s.prepareTLSConfig()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.srv.TLSConfig = tlsCfg
	hasTLS := tlsCfg != nil

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", s.srv.Addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	s.addr = lis.Addr().String()
	s.state = serverRunning
	s.serveErr = nil
	s.done = done
	s.wg.Go(func() {
		defer close(done)
		defer lis.Close()
		var serveErr error
		if hasTLS {
			serveErr = s.srv.ServeTLS(lis, "", "")
		} else {
			serveErr = s.srv.Serve(lis)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.mu.Lock()
			if s.state == serverRunning {
				s.state = serverFailed
				s.serveErr = serveErr
			}
			s.mu.Unlock()
			s.opts.logger.Error("http server serve failed", slog.Any("error", serveErr))
		}
	})
	s.mu.Unlock()

	s.opts.logger.InfoContext(ctx, fmt.Sprintf("%s server listening", s.opts.name),
		slog.String("addr", s.addr),
		slog.Bool("tls", hasTLS),
	)
	return nil
}

func (s *Server) prepareTLSConfig() (*tls.Config, error) {
	var tlsCfg *tls.Config
	if s.opts.tlsConfig != nil {
		tlsCfg = s.opts.tlsConfig.Clone()
	}
	if s.opts.tlsCertFilesSet {
		if s.opts.tlsCertFile == "" || s.opts.tlsKeyFile == "" {
			return nil, errors.New("http: both TLS certFile and keyFile are required")
		}
		cert, err := tls.LoadX509KeyPair(s.opts.tlsCertFile, s.opts.tlsKeyFile)
		if err != nil {
			return nil, fmt.Errorf("http: load TLS key pair: %w", err)
		}
		if tlsCfg == nil {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		tlsCfg.Certificates = append(tlsCfg.Certificates, cert)
	}
	if tlsCfg != nil && len(tlsCfg.Certificates) == 0 && tlsCfg.GetCertificate == nil && tlsCfg.GetConfigForClient == nil {
		return nil, errors.New("http: TLS config requires certificates, GetCertificate, or GetConfigForClient")
	}
	return tlsCfg, nil
}

// Stop gracefully shuts down the server and waits for active requests and Serve to exit.
// If ctx expires before in-flight requests drain, the server remains draining so Stop can be retried.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	switch s.state {
	case serverIdle, serverClosed:
		s.mu.Unlock()
		return nil
	case serverFailed:
		s.mu.Unlock()
		s.wg.Wait()
		s.mu.Lock()
		if s.state == serverFailed {
			s.state = serverClosed
			s.serveErr = nil
		}
		s.mu.Unlock()
		return nil
	}
	s.state = serverDraining
	s.mu.Unlock()

	if err := s.srv.Shutdown(ctx); err != nil {
		return err
	}
	s.wg.Wait()
	s.mu.Lock()
	s.state = serverClosed
	s.serveErr = nil
	s.mu.Unlock()
	return nil
}

// HealthCheck reports whether the server is actively listening.
func (s *Server) HealthCheck(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case serverRunning:
		return nil
	case serverFailed:
		return fmt.Errorf("http: server failed: %w", s.serveErr)
	case serverDraining:
		return errors.New("http: server is shutting down")
	default:
		return errors.New("http: server is not running")
	}
}

// Done returns a channel closed when the serve goroutine exits, or nil before Start.
func (s *Server) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// Err returns the unexpected Serve error if the server failed.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == serverFailed {
		return s.serveErr
	}
	return nil
}
