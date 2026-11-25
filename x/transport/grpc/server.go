// Package grpc provides a hardened gRPC server wrapper with OTel tracing/metrics,
// slog logging, panic recovery, fault-to-status mapping, health v1, and reflection.
package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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
	return func(o *options) {
		for _, opt := range opts {
			if opt != nil {
				o.serverOpts = append(o.serverOpts, opt)
			}
		}
	}
}

func WithUnaryInterceptor(ints ...grpc.UnaryServerInterceptor) Option {
	return func(o *options) {
		for _, in := range ints {
			if in != nil {
				o.unaryInts = append(o.unaryInts, in)
			}
		}
	}
}

func WithStreamInterceptor(ints ...grpc.StreamServerInterceptor) Option {
	return func(o *options) {
		for _, in := range ints {
			if in != nil {
				o.streamInts = append(o.streamInts, in)
			}
		}
	}
}

func WithShutdownTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.shutdownTimeout = timeout
		}
	}
}

type serverState uint8

const (
	serverIdle serverState = iota
	serverRunning
	serverStopping
	serverFailed
	serverClosed
)

// Server wraps *grpc.Server with Lazy startup, Start/Stop, Run(ctx) for cx.Runner,
// and HealthCheck integration.
type Server struct {
	srv      *grpc.Server
	health   *health.Server
	opts     options
	mu       sync.Mutex
	addr     string
	state    serverState
	serveErr error
	done     chan struct{}
	stopOnce sync.Once
	stopDone chan struct{}
	wg       sync.WaitGroup
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
		logging.UnaryServerInterceptor(logAdapter),
		recovery.UnaryServerInterceptor(recHandler),
		faultUnaryInterceptor(),
	}, o.unaryInts...)

	stream := append([]grpc.StreamServerInterceptor{
		logging.StreamServerInterceptor(logAdapter),
		recovery.StreamServerInterceptor(recHandler),
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
	switch s.state {
	case serverIdle:
	case serverClosed, serverFailed:
		return nil, errors.New("grpc: server already stopped")
	default:
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
	done := make(chan struct{})
	s.addr = lis.Addr().String()
	s.state = serverRunning
	s.serveErr = nil
	s.done = done
	s.stopDone = make(chan struct{})

	serveErr := make(chan error, 1)
	s.wg.Go(func() {
		defer close(done)
		err := s.srv.Serve(lis)
		s.mu.Lock()
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.serveErr = err
			if s.state == serverRunning {
				s.state = serverFailed
			}
			s.mu.Unlock()
			s.opts.logger.Error("grpc server serve failed", slog.Any("error", err))
		} else {
			if s.state == serverRunning {
				s.state = serverClosed
			}
			s.mu.Unlock()
		}
		serveErr <- err
	})
	s.opts.logger.InfoContext(ctx, "grpc server listening", slog.String("addr", s.addr))
	return serveErr, nil
}

// Stop gracefully stops the gRPC server, falling back to hard Stop if ctx expires.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	switch s.state {
	case serverIdle:
		s.mu.Unlock()
		return nil
	case serverClosed:
		s.mu.Unlock()
		s.wg.Wait()
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
	case serverRunning:
		s.state = serverStopping
	case serverStopping:
	}
	stopDone := s.stopDone
	s.mu.Unlock()

	s.stopOnce.Do(func() {
		if s.health != nil {
			s.health.Shutdown()
		}
		go func() {
			s.srv.GracefulStop()
			close(stopDone)
		}()
	})

	var ctxErr error
	select {
	case <-stopDone:
	case <-ctx.Done():
		ctxErr = ctx.Err()
		s.srv.Stop()
		<-stopDone
	}
	s.wg.Wait()

	s.mu.Lock()
	s.state = serverClosed
	s.serveErr = nil
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
	switch s.state {
	case serverRunning:
		return nil
	case serverFailed:
		return fmt.Errorf("grpc: server failed: %w", s.serveErr)
	case serverStopping:
		return errors.New("grpc: server is shutting down")
	default:
		return errors.New("grpc: server is not running")
	}
}

// Done returns a channel closed when Serve exits, or nil before Start/Run.
func (s *Server) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// Err returns the unexpected Serve error if Serve failed.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == serverFailed {
		return s.serveErr
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
