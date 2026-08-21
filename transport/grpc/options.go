package grpc

import (
	"crypto/tls"

	"google.golang.org/grpc"
)

// options holds the builder state for NewServer.
type options struct {
	addr               string
	name               string
	tlsConfig          *tls.Config
	unaryInterceptors  []grpc.UnaryServerInterceptor
	streamInterceptors []grpc.StreamServerInterceptor
	serverOptions      []grpc.ServerOption
}

// Option configures a Server.
type Option func(*options)

// WithAddr sets the TCP address the server listens on (e.g. ":50051").
func WithAddr(addr string) Option { return func(o *options) { o.addr = addr } }

// WithName sets the server name, used in log output.
func WithName(name string) Option { return func(o *options) { o.name = name } }

// WithTLSConfig enables TLS using the provided *tls.Config.
func WithTLSConfig(tlsCfg *tls.Config) Option {
	return func(o *options) { o.tlsConfig = tlsCfg }
}

// WithUnaryInterceptor appends unary server interceptors applied in order.
func WithUnaryInterceptor(interceptors ...grpc.UnaryServerInterceptor) Option {
	return func(o *options) {
		o.unaryInterceptors = append(o.unaryInterceptors, interceptors...)
	}
}

// WithStreamInterceptor appends stream server interceptors applied in order.
func WithStreamInterceptor(interceptors ...grpc.StreamServerInterceptor) Option {
	return func(o *options) {
		o.streamInterceptors = append(o.streamInterceptors, interceptors...)
	}
}

// WithServerOption appends raw grpc.ServerOption values.
func WithServerOption(opts ...grpc.ServerOption) Option {
	return func(o *options) {
		o.serverOptions = append(o.serverOptions, opts...)
	}
}
