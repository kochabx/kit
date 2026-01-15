package metrics

import "github.com/prometheus/client_golang/prometheus"

type Metrics interface {
	Registry() *prometheus.Registry
}
