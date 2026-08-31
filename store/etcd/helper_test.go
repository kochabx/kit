package etcd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLockValidation(t *testing.T) {
	client, err := New(&Config{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	lock, err := client.NewLock("", time.Second)
	assert.Nil(t, lock)
	assert.ErrorIs(t, err, ErrInvalidConfig)
	lock, err = client.NewLock("lock", 0)
	assert.Nil(t, lock)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRegistryValidation(t *testing.T) {
	client, err := New(&Config{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	registry, err := client.NewRegistry("/", time.Second)
	assert.Nil(t, registry)
	assert.ErrorIs(t, err, ErrInvalidConfig)
	registry, err = client.NewRegistry("services", 0)
	assert.Nil(t, registry)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestLockIntegration(t *testing.T) {
	requireIntegration(t)
	client, err := New(testConfig())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	first, err := client.NewLock("kit/locks/test", 5*time.Second)
	require.NoError(t, err)
	second, err := client.NewLock("kit/locks/test", 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, first.TryLock(t.Context()))
	assert.ErrorIs(t, first.TryLock(t.Context()), ErrLockHeld)
	assert.ErrorIs(t, second.TryLock(t.Context()), ErrLockHeld)
	require.NoError(t, first.Unlock(t.Context()))
	require.NoError(t, first.Unlock(t.Context()))
	require.NoError(t, second.TryLock(t.Context()))
	require.NoError(t, second.Unlock(t.Context()))
}

func TestRegistryIntegration(t *testing.T) {
	requireIntegration(t)
	client, err := New(testConfig())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	registry, err := client.NewRegistry("kit/services", 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, registry.Register(t.Context(), "api", "127.0.0.1:8080"))
	assert.ErrorIs(t, registry.Register(t.Context(), "other", "value"), ErrServiceExists)

	services, err := registry.Services(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"api": "127.0.0.1:8080"}, services)
	require.NoError(t, registry.Deregister(t.Context()))
	require.NoError(t, registry.Deregister(t.Context()))

	services, err = registry.Services(t.Context())
	require.NoError(t, err)
	assert.Empty(t, services)
}
