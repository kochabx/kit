// Package otel provides lazy, lifecycle-managed OpenTelemetry trace/metric SDK
// initialization and a trace-context extractor for kit/log.
package otel

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/kochabx/kit/log"
)

var (
	ErrInvalidConfig  = errors.New("invalid otel config")
	ErrAlreadyStarted = errors.New("otel provider already started")
	ErrNotStarted     = errors.New("otel provider not started")
	ErrClosed         = errors.New("otel provider closed")
)

// Config configures OTLP trace and metric exporters using HTTP transport.
type Config struct {
	ServiceName    string            `json:"service_name"`
	ServiceVersion string            `json:"service_version,omitzero"`
	Environment    string            `json:"environment,omitzero"`
	Endpoint       string            `json:"endpoint,omitzero"`
	Insecure       bool              `json:"insecure,omitzero"`
	SampleRatio    *float64          `json:"sample_ratio,omitzero"`
	EnableTrace    *bool             `json:"enable_trace,omitzero"`
	EnableMetric   *bool             `json:"enable_metric,omitzero"`
	Headers        map[string]string `json:"headers,omitzero"`
	TLS            *tls.Config       `json:"-"`
}

func (c Config) traceEnabled() bool  { return c.EnableTrace == nil || *c.EnableTrace }
func (c Config) metricEnabled() bool { return c.EnableMetric == nil || *c.EnableMetric }
func (c Config) sampleRatio() float64 {
	if c.SampleRatio == nil {
		return 1.0
	}
	return *c.SampleRatio
}

// Validate checks Config invariants using pure Go validation (zero reflection).
func (c Config) Validate() error {
	var errs []error
	if c.ServiceName == "" {
		errs = append(errs, fmt.Errorf("%w: service_name is required", ErrInvalidConfig))
	}
	if (c.traceEnabled() || c.metricEnabled()) && c.Endpoint == "" {
		errs = append(errs, fmt.Errorf("%w: endpoint is required when trace or metric is enabled", ErrInvalidConfig))
	}
	if c.Insecure && c.TLS != nil {
		errs = append(errs, fmt.Errorf("%w: insecure cannot be combined with TLS config", ErrInvalidConfig))
	}
	if ratio := c.sampleRatio(); math.IsNaN(ratio) || ratio < 0 || ratio > 1 {
		errs = append(errs, fmt.Errorf("%w: sample_ratio must be in [0, 1], got %v", ErrInvalidConfig, ratio))
	}
	return errors.Join(errs...)
}

type options struct {
	logger         *slog.Logger
	attrs          []attribute.KeyValue
	registerGlobal bool
}

// Option customizes Provider construction.
type Option func(*options)

func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

func WithResourceAttributes(attrs ...attribute.KeyValue) Option {
	return func(o *options) { o.attrs = append(o.attrs, attrs...) }
}

func WithRegisterGlobal(enabled bool) Option {
	return func(o *options) { o.registerGlobal = enabled }
}

// Provider owns lazy OTel TracerProvider and MeterProvider lifecycles.
type Provider struct {
	cfg            Config
	logger         *slog.Logger
	attrs          []attribute.KeyValue
	registerGlobal bool

	stopMu  sync.Mutex
	mu      sync.Mutex
	started bool
	closed  bool
	tracer  *sdktrace.TracerProvider
	meter   *sdkmetric.MeterProvider
}

// New validates cfg and creates a Lazy Provider (establishing no network connections).
func New(cfg Config, opts ...Option) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.TLS != nil {
		cfg.TLS = cfg.TLS.Clone()
	}
	cfg.Headers = maps.Clone(cfg.Headers)

	opt := options{
		logger:         slog.New(slog.DiscardHandler),
		registerGlobal: true,
	}
	for _, fn := range opts {
		if fn != nil {
			fn(&opt)
		}
	}
	p := &Provider{
		cfg:            cfg,
		logger:         opt.logger,
		attrs:          slices.Clone(opt.attrs),
		registerGlobal: opt.registerGlobal,
	}
	return p, nil
}

// Start initializes OTel exporters and providers, registering globals when configured.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidConfig)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if p.started {
		return ErrAlreadyStarted
	}

	res := p.buildResource()

	tp, err := p.initTracerProvider(ctx, res)
	if err != nil {
		return err
	}
	mp, err := p.initMeterProvider(ctx, res)
	if err != nil {
		if tp != nil {
			_ = tp.Shutdown(ctx)
		}
		return err
	}

	p.tracer = tp
	p.meter = mp
	p.started = true

	if p.registerGlobal {
		if tp != nil {
			otel.SetTracerProvider(tp)
		}
		if mp != nil {
			otel.SetMeterProvider(mp)
		}
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	}
	p.logger.InfoContext(ctx, "otel provider started",
		slog.String("service", p.cfg.ServiceName),
		slog.Bool("trace", tp != nil),
		slog.Bool("metric", mp != nil),
	)
	return nil
}

func (p *Provider) buildResource() *resource.Resource {
	resAttrs := []attribute.KeyValue{semconv.ServiceName(p.cfg.ServiceName)}
	if v := cmp.Or(p.cfg.ServiceVersion, ""); v != "" {
		resAttrs = append(resAttrs, semconv.ServiceVersion(v))
	}
	if env := cmp.Or(p.cfg.Environment, ""); env != "" {
		resAttrs = append(resAttrs, attribute.String("deployment.environment.name", env))
	}
	resAttrs = append(resAttrs, p.attrs...)
	return resource.NewWithAttributes(semconv.SchemaURL, resAttrs...)
}

func (p *Provider) initTracerProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	if !p.cfg.traceEnabled() {
		return nil, nil
	}
	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(signalEndpointURL(p.cfg.Endpoint, "/v1/traces"))}
	if p.cfg.Endpoint != "" && !strings.Contains(p.cfg.Endpoint, "://") {
		traceOpts = []otlptracehttp.Option{otlptracehttp.WithEndpoint(p.cfg.Endpoint)}
		if p.cfg.Insecure {
			traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		}
	}
	if len(p.cfg.Headers) > 0 {
		traceOpts = append(traceOpts, otlptracehttp.WithHeaders(p.cfg.Headers))
	}
	if p.cfg.TLS != nil {
		traceOpts = append(traceOpts, otlptracehttp.WithTLSClientConfig(p.cfg.TLS))
	}
	exp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("otel: init trace exporter: %w", err)
	}
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(p.cfg.sampleRatio()))
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	), nil
}

func (p *Provider) initMeterProvider(ctx context.Context, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	if !p.cfg.metricEnabled() {
		return nil, nil
	}
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(signalEndpointURL(p.cfg.Endpoint, "/v1/metrics"))}
	if p.cfg.Endpoint != "" && !strings.Contains(p.cfg.Endpoint, "://") {
		metricOpts = []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(p.cfg.Endpoint)}
		if p.cfg.Insecure {
			metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
		}
	}
	if len(p.cfg.Headers) > 0 {
		metricOpts = append(metricOpts, otlpmetrichttp.WithHeaders(p.cfg.Headers))
	}
	if p.cfg.TLS != nil {
		metricOpts = append(metricOpts, otlpmetrichttp.WithTLSClientConfig(p.cfg.TLS))
	}
	exp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("otel: init metric exporter: %w", err)
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		sdkmetric.WithResource(res),
	), nil
}

// Stop flushes and shuts down active OTel providers respecting ctx.
func (p *Provider) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.stopMu.Lock()
	defer p.stopMu.Unlock()

	p.mu.Lock()
	if !p.started || p.closed {
		p.closed = true
		p.mu.Unlock()
		return nil
	}
	tp, mp := p.tracer, p.meter
	p.mu.Unlock()

	var errs []error
	if tp != nil {
		errs = append(errs, tp.Shutdown(ctx))
	}
	if mp != nil {
		errs = append(errs, mp.Shutdown(ctx))
	}
	err := errors.Join(errs...)
	p.mu.Lock()
	if err == nil {
		p.closed = true
	}
	p.mu.Unlock()
	return err
}

// Close idempotently stops the Provider.
func (p *Provider) Close() error { return p.Stop(context.Background()) }

// HealthCheck verifies the provider is started and open.
func (p *Provider) HealthCheck(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if !p.started {
		return ErrNotStarted
	}
	return nil
}

func (p *Provider) TracerProvider() trace.TracerProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tracer != nil {
		return p.tracer
	}
	return otel.GetTracerProvider()
}

func (p *Provider) MeterProvider() metric.MeterProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.meter != nil {
		return p.meter
	}
	return otel.GetMeterProvider()
}

func signalEndpointURL(rawEndpoint, defaultPath string) string {
	u, err := url.Parse(rawEndpoint)
	if err != nil || u.Scheme == "" {
		return rawEndpoint
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = defaultPath
		return u.String()
	}
	return rawEndpoint
}

// LogExtractor returns a kit/log ContextExtractor attaching trace_id and span_id from ctx.
func LogExtractor() log.ContextExtractor {
	return func(ctx context.Context) []slog.Attr {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return nil
		}
		return []slog.Attr{
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		}
	}
}
