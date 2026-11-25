package asynq

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type otelConfig struct {
	tp trace.TracerProvider
	mp metric.MeterProvider
}

// OTelOption configures OTelMiddleware.
type OTelOption func(*otelConfig)

// WithTracerProvider overrides the global OpenTelemetry TracerProvider.
func WithTracerProvider(tp trace.TracerProvider) OTelOption {
	return func(c *otelConfig) {
		if tp != nil {
			c.tp = tp
		}
	}
}

// WithMeterProvider overrides the global OpenTelemetry MeterProvider.
func WithMeterProvider(mp metric.MeterProvider) OTelOption {
	return func(c *otelConfig) {
		if mp != nil {
			c.mp = mp
		}
	}
}

// OTelMiddleware instruments asynq task handlers with OpenTelemetry tracing spans and duration metrics.
func OTelMiddleware(opts ...OTelOption) asynq.MiddlewareFunc {
	var cfg otelConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) (err error) {
			tp := cfg.tp
			if tp == nil {
				tp = otel.GetTracerProvider()
			}
			mp := cfg.mp
			if mp == nil {
				mp = otel.GetMeterProvider()
			}
			tracer := tp.Tracer("github.com/kochabx/kit/x/asynq")
			durHist, _ := mp.Meter("github.com/kochabx/kit/x/asynq").Float64Histogram(
				"asynq.process.duration",
				metric.WithUnit("s"),
			)

			attrs := []attribute.KeyValue{
				attribute.String("asynq.task.type", task.Type()),
			}
			if id, ok := asynq.GetTaskID(ctx); ok {
				attrs = append(attrs, attribute.String("asynq.task.id", id))
			}
			if q, ok := asynq.GetQueueName(ctx); ok {
				attrs = append(attrs, attribute.String("asynq.queue", q))
			}
			if retry, ok := asynq.GetRetryCount(ctx); ok {
				attrs = append(attrs, attribute.Int("asynq.retry_count", retry))
			}

			ctx, span := tracer.Start(ctx, "asynq.process "+task.Type(),
				trace.WithSpanKind(trace.SpanKindConsumer),
				trace.WithAttributes(attrs...),
			)
			start := time.Now()
			defer func() {
				if recovered := recover(); recovered != nil {
					span.SetStatus(codes.Error, "task handler panicked")
					if durHist != nil {
						durHist.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
							attribute.String("asynq.task.type", task.Type()),
							attribute.String("asynq.status", "panic"),
						))
					}
					span.End()
					panic(recovered)
				}
				statusAttr := attribute.String("asynq.status", "ok")
				if err != nil {
					statusAttr = attribute.String("asynq.status", "error")
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				if durHist != nil {
					durHist.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
						attribute.String("asynq.task.type", task.Type()),
						statusAttr,
					))
				}
				span.End()
			}()

			return next.ProcessTask(ctx, task)
		})
	}
}
