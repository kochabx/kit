package etcd

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func testEndpoint() string {
	if endpoint := os.Getenv("ETCD_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	return "localhost:2379"
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("KIT_ETCD_INTEGRATION") == "" {
		t.Skip("set KIT_ETCD_INTEGRATION=1 to run etcd integration tests")
	}
}

func testConfig() *Config { return &Config{Endpoints: []string{testEndpoint()}} }

func TestResolveConfigDefaultsWithoutMutation(t *testing.T) {
	input := Config{}
	resolved, err := resolveConfig(input)
	require.NoError(t, err)
	assert.Empty(t, input.Endpoints)
	assert.Equal(t, []string{"localhost:2379"}, resolved.Endpoints)
	assert.Equal(t, 5*time.Second, resolved.DialTimeout)
	assert.Equal(t, 30*time.Second, resolved.KeepAliveTime)
	assert.Equal(t, 5*time.Second, resolved.KeepAliveTimeout)
}

func TestResolveConfigClonesMutableValues(t *testing.T) {
	tlsConfig := &tls.Config{ServerName: "etcd.internal", MinVersion: tls.VersionTLS13}
	input := Config{Endpoints: []string{"etcd.internal:2379"}, TLS: tlsConfig}
	resolved, err := resolveConfig(input)
	require.NoError(t, err)
	resolved.Endpoints[0] = "changed:2379"
	resolved.TLS.ServerName = "changed"
	assert.Equal(t, "etcd.internal:2379", input.Endpoints[0])
	assert.Equal(t, "etcd.internal", tlsConfig.ServerName)
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "nil config"},
		{name: "empty endpoint", cfg: &Config{Endpoints: []string{""}}},
		{name: "negative auto sync", cfg: &Config{AutoSyncInterval: -time.Second}},
		{name: "negative send size", cfg: &Config{MaxSendMsgSize: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.cfg)
			assert.Nil(t, client)
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestNewCreatesLazyClient(t *testing.T) {
	client, err := New(&Config{Endpoints: []string{"127.0.0.1:1"}})
	require.NoError(t, err)
	assert.IsType(t, &clientv3.Client{}, client.Client())
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
}

func TestPingPreservesErrorChain(t *testing.T) {
	client, err := New(&Config{Endpoints: []string{"127.0.0.1:1"}})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err = client.Ping(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestIntegrationLifecycleAndOperations(t *testing.T) {
	requireIntegration(t)
	client, err := New(testConfig())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	require.NoError(t, client.Start(t.Context()))
	require.NoError(t, client.HealthCheck(t.Context()))

	_, err = client.Client().Put(t.Context(), "kit/etcd/test", "value")
	require.NoError(t, err)
	response, err := client.Client().Get(t.Context(), "kit/etcd/test")
	require.NoError(t, err)
	require.Len(t, response.Kvs, 1)
	assert.Equal(t, "value", string(response.Kvs[0].Value))
	_, err = client.Client().Delete(t.Context(), "kit/etcd/test")
	require.NoError(t, err)
}
