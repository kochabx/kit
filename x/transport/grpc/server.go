// Package grpc provides a hardened gRPC server wrapper with OTel tracing/metrics,
// slog logging, panic recovery, fault-to-status mapping, health v1, and reflection.
package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/kochabx/kit/fault"
)

const defaultShutdownTimeout = 15 * time.Second

type options struct {
	addr            string
	tlsConfig       *tls.Config
	logger          *slog.Logger
	otelEnabled     bool
	reflection      bool
	healthEnabled   bool
	shutdownTimeout time.Duration
	serverOpts      []grpc.ServerOption
	unaryInts       []grpc.UnaryServerInterceptor
	streamInts      []grpc.StreamServerInterceptor
}

// Option customizes Server construction.
type Option func(*options)

func WithAddr(addr string) Option {
	return func(o *options) {
		if addr != "" {
			o.addr = addr
		}
	}
}

func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(o *options) {
		if tlsCfg != nil {
			o.tlsConfig = tlsCfg.Clone()
		}
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

func WithOTel(enabled bool) Option {
	return func(o *options) { o.otelEnabled = enabled }
}

func WithReflection(enabled bool) Option {
	return func(o *options) { o.reflection = enabled }
}

func WithHealthServer(enabled bool) Option {
	return func(o *options) { o.healthEnabled = enabled }
}

func WithServerOptions(opts ...grpc.ServerOption) Option {
	return func(o *options) { o.serverOpts = append(o.serverOpts, opts...) }
}

func WithUnaryInterceptor(ints ...grpc.UnaryServerInterceptor) Option {
	return func(o *options) { o.unaryInts = append(o.unaryInts, ints...) }
}

func WithStreamInterceptor(ints ...grpc.StreamServerInterceptor) Option {
	return func(o *options) { o.streamInts = append(o.streamInts, ints...) }
}

func WithShutdownTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.shutdownTimeout = timeout
		}
	}
}

// Server wraps *grpc.Server with Lazy startup, Start/Stop, Run(ctx) for cx.Runner,
// and HealthCheck integration.
type Server struct {
	srv     *grpc.Server
	health  *health.Server
	opts    options
	mu      sync.Mutex
	addr    string
	running bool
	stopped bool
	wg      sync.WaitGroup
}

// NewServer constructs a gRPC Server (does not bind any socket until Start or Run).
func NewServer(opts ...Option) *Server {
	o := options{
		addr:            ":9090",
		logger:          slog.New(slog.DiscardHandler),
		otelEnabled:     true,
		reflection:      true,
		healthEnabled:   true,
		shutdownTimeout: defaultShutdownTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	logAdapter := logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		o.logger.Log(ctx, slog.Level(lvl), msg, fields...)
	})
	recHandler := recovery.WithRecoveryHandlerContext(func(ctx context.Context, p any) error {
		o.logger.ErrorContext(ctx, "grpc panic recovered",
			slog.Any("panic", p),
			slog.String("stack", string(debug.Stack())),
		)
		return status.Error(codes.Internal, "internal server error")
	})

	unary := append([]grpc.UnaryServerInterceptor{
		recovery.UnaryServerInterceptor(recHandler),
		logging.UnaryServerInterceptor(logAdapter),
		faultUnaryInterceptor(),
	}, o.unaryInts...)

	stream := append([]grpc.StreamServerInterceptor{
		recovery.StreamServerInterceptor(recHandler),
		logging.StreamServerInterceptor(logAdapter),
		faultStreamInterceptor(),
	}, o.streamInts...)

	srvOpts := make([]grpc.ServerOption, 0, len(o.serverOpts)+4)
	if o.tlsConfig != nil {
		srvOpts = append(srvOpts, grpc.Creds(credentials.NewTLS(o.tlsConfig)))
	}
	if o.otelEnabled {
		srvOpts = append(srvOpts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}
	srvOpts = append(srvOpts, grpc.ChainUnaryInterceptor(unary...), grpc.ChainStreamInterceptor(stream...))
	srvOpts = append(srvOpts, o.serverOpts...)

	gs := grpc.NewServer(srvOpts...)
	var hs *health.Server
	if o.healthEnabled {
		hs = health.NewServer()
		healthpb.RegisterHealthServer(gs, hs)
	}
	if o.reflection {
		reflection.Register(gs)
	}
	return &Server{srv: gs, health: hs, opts: o}
}

// GRPCServer exposes the underlying *grpc.Server for service registration before Start/Run.
func (s *Server) GRPCServer() *grpc.Server { return s.srv }

// Addr returns the bound listener address after Start/Run, or configured Addr beforehand.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.addr != "" {
		return s.addr
	}
	return s.opts.addr
}

// Start opens the TCP listener and serves gRPC requests in the background.
func (s *Server) Start(ctx context.Context) error {
	_, err := s.startListen(ctx)
	return err
}

func (s *Server) startListen(ctx context.Context) (<-chan error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, errors.New("grpc: server already stopped")
	}
	if s.running {
		return nil, errors.New("grpc: server already started")
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", s.opts.addr)
	if err != nil {
		return nil, err
	}
	if s.health != nil {
		s.health.Resume()
	}
	s.addr = lis.Addr().String()
	s.running = true

	serveErr := make(chan error, 1)
	s.wg.Go(func() {
		err := s.srv.Serve(lis)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.opts.logger.Error("grpc server serve failed", slog.Any("error", err))
		}
		serveErr <- err
	})
	s.opts.logger.InfoContext(ctx, "grpc server listening", slog.String("addr", s.addr))
	return serveErr, nil
}

// Stop gracefully stops the gRPC server, falling back to hard Stop if ctx expires.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	if s.health != nil {
		s.health.Shutdown()
	}

	stopped := make(chan struct{})
	go func() {
		s.srv.GracefulStop()
		close(stopped)
	}()

	var ctxErr error
	select {
	case <-stopped:
	case <-ctx.Done():
		ctxErr = ctx.Err()
		s.srv.Stop()
		<-stopped
	}
	s.wg.Wait()

	s.mu.Lock()
	s.running = false
	s.stopped = true
	s.mu.Unlock()
	return ctxErr
}

// Run starts the server and blocks until ctx is canceled or Serve fails (for cx.MustRunner).
func (s *Server) Run(ctx context.Context) error {
	serveErr, err := s.startListen(ctx)
	if err != nil {
		return err
	}
	select {
	case err := <-serveErr:
		s.wg.Wait()
		s.mu.Lock()
		s.running = false
		s.stopped = true
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.shutdownTimeout)
		defer cancel()
		_ = s.Stop(stopCtx)
		return ctx.Err()
	}
}

// HealthCheck reports whether the gRPC server is active.
func (s *Server) HealthCheck(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return errors.New("grpc: server is not running")
	}
	return nil
}

func faultUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		return resp, mapError(err)
	}
}

func faultStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return mapError(handler(srv, ss))
	}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[interface {
		error
		GRPCStatus() *status.Status
	}](err); ok {
		return err
	}
	if f, ok := fault.From(err); ok {
		return status.Error(codes.Code(f.GRPCCode()), f.Message())
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, context.Canceled.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	}
	return status.Error(codes.Internal, "internal server error")
}
