package scheduler

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"net"
	"testing"
	"time"
)

func TestBorrowedClientRequiresContextTimeouts(t *testing.T) {
	r := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer r.Close()
	if _, err := NewClient(r, ClientConfig{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unbounded client accepted: %v", err)
	}
	if _, err := NewWorker(r, WorkerConfig{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unbounded worker client accepted: %v", err)
	}
	if r.Options().ContextTimeoutEnabled {
		t.Fatal("constructor mutated borrowed options")
	}
}

func TestWorkerManagementReusesQueueStores(t *testing.T) {
	r := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", ContextTimeoutEnabled: true})
	defer r.Close()
	w, err := NewWorker(r, WorkerConfig{History: HistoryConfig{MaxEntries: 10}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for name, q := range w.runtime.queues {
		if q.store != w.runtime.queueStores[name].store {
			t.Fatal("duplicate queue store", name)
		}
		if q.store.HistoryLimit != 10 || q.store.ClaimIdle != w.runtime.config.Execution.LeaseDuration {
			t.Fatal("worker store options lost")
		}
	}
}

func TestOperationTimeoutBoundsUnresponsiveRedis(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	defer close(serverDone)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-serverDone
	}()
	r := redis.NewClient(&redis.Options{
		Addr: listener.Addr().
			String(),
		ContextTimeoutEnabled: true,
		MaxRetries:            -1,
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          10 * time.Second,
	})
	defer r.Close()
	c, err := NewClient(r, ClientConfig{OperationTimeout: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	started := time.Now()
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("unresponsive server succeeded")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("operation exceeded deadline: %v", elapsed)
	}
}
