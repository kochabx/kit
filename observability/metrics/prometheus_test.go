package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_WithCollectors(t *testing.T) {
	p := New(WithGoCollectorRuntimeMetrics(), WithBuildInfoCollector())
	require.NotNil(t, p)

	metricFamilies, err := p.Registry().Gather()
	require.NoError(t, err)

	names := make(map[string]struct{}, len(metricFamilies))
	for _, mf := range metricFamilies {
		names[mf.GetName()] = struct{}{}
	}

	assert.Contains(t, names, "go_info")
	assert.Contains(t, names, "go_build_info")
}

func TestCollectorRegistrationPanicsOnDuplicate(t *testing.T) {
	assert.Panics(t, func() {
		New(WithBuildInfoCollector(), WithBuildInfoCollector())
	})
}

func TestDefaultPrometheusInstance(t *testing.T) {
	require.NotNil(t, Prom)
	assert.NotNil(t, Prom.Registry())
}

func TestWithRegistry(t *testing.T) {
	registry := prometheus.NewRegistry()
	p := New(WithRegistry(registry))

	assert.Same(t, registry, p.Registry())
}
