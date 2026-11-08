package redis_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler"
	kitredis "github.com/kochabx/kit/store/redis"
	"github.com/stretchr/testify/require"
)

func TestClientsSupportSchedulerContexts(t *testing.T) {
	for _, cfg := range []*kitredis.Config{
		kitredis.Single("127.0.0.1:6379"),
		kitredis.Cluster("127.0.0.1:6379"),
		kitredis.Sentinel("master", "127.0.0.1:26379"),
	} {
		t.Run(string(cfg.Mode), func(t *testing.T) {
			client, err := kitredis.New(cfg)
			require.NoError(t, err)
			defer client.Close()
			producer, err := scheduler.NewClient(client.UniversalClient(), scheduler.ClientConfig{})
			require.NoError(t, err)
			defer producer.Close()
			worker, err := scheduler.NewWorker(client.UniversalClient(), scheduler.WorkerConfig{})
			require.NoError(t, err)
			require.NoError(t, worker.Close())
		})
	}
}

func TestCommandDeadlineBoundsUnresponsiveConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	release, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-release
	}()
	t.Cleanup(func() { close(release); _ = listener.Close(); <-done })
	cfg := kitredis.Single(listener.Addr().String())
	cfg.ReadTimeout = 3 * time.Second
	cfg.WriteTimeout = 3 * time.Second
	cfg.MaxRetries = -1
	client, err := kitredis.New(cfg)
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = client.Ping(ctx)
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second, "command must honor context deadline rather than the 3s socket timeout")
}
