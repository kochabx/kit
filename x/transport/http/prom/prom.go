// Package prom provides Prometheus HTTP request metrics middleware and scrape endpoint.
package prom

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	kithttp "github.com/kochabx/kit/x/transport/http"
)

type options struct {
	namespace  string
	registerer prometheus.Registerer
}

// Option customizes Prometheus middleware metrics.
type Option func(*options)

func WithNamespace(ns string) Option {
	return func(o *options) { o.namespace = ns }
}

func WithRegisterer(reg prometheus.Registerer) Option {
	return func(o *options) {
		if reg != nil {
			o.registerer = reg
		}
	}
}

// Middleware records http_requests_total and http_request_duration_seconds per route.
func Middleware(opts ...Option) (func(http.Handler) http.Handler, error) {
	o := options{registerer: prometheus.DefaultRegisterer}
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}

	reqTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: o.namespace,
		Name:      "http_requests_total",
		Help:      "Total HTTP requests processed.",
	}, []string{"method", "path", "status"})

	reqDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: o.namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency distribution.",
	}, []string{"method", "path"})

	if err := o.registerer.Register(reqTotal); err != nil {
		return nil, err
	}
	if err := o.registerer.Register(reqDuration); err != nil {
		o.registerer.Unregister(reqTotal)
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := kithttp.NewResponseRecorder(w)
			next.ServeHTTP(rec, r)

			pattern := r.Pattern
			if pattern == "" {
				pattern = "UNMATCHED"
			}
			reqTotal.WithLabelValues(r.Method, pattern, strconv.Itoa(rec.Status())).Inc()
			reqDuration.WithLabelValues(r.Method, pattern).Observe(time.Since(start).Seconds())
		})
	}, nil
}

// Handler mounts a Prometheus scrape endpoint backed by gatherer.
func Handler(gatherer prometheus.Gatherer) http.Handler {
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
