package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kochabx/kit/log"
)

type prefixCodec struct{ prefix string }

func (c prefixCodec) Identity() string                 { return "prefix:" + c.prefix }
func (c prefixCodec) Marshal(v string) ([]byte, error) { return []byte(c.prefix + v), nil }
func (c prefixCodec) Unmarshal(v []byte) (string, error) {
	if !strings.HasPrefix(string(v), c.prefix) {
		return "", errors.New("wrong encoding")
	}
	return strings.TrimPrefix(string(v), c.prefix), nil
}

func TestFingerprintIncludesCodecIdentityAndVersion(t *testing.T) {
	a, err := NewTaskWithCodec[string]("encoded", prefixCodec{"v1:"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewTaskWithCodec[string]("encoded", prefixCodec{"v2:"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewTaskWithCodec[string]("encoded", prefixCodec{"v1:"}, WithVersion("2"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() == b.Fingerprint() || a.Fingerprint() == c.Fingerprint() {
		t.Fatal("wire identity or version missing from fingerprint")
	}
	if a.Version() != "1" || c.Version() != "2" {
		t.Fatal("incorrect version")
	}
	if _, err := NewTask[int]("version", WithVersion("")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty version: %v", err)
	}
}

func TestConfigOwnsSlices(t *testing.T) {
	input := WorkerConfig{Queues: []QueueConfig{{Name: "a"}}, Execution: ExecutionConfig{Queues: []string{"a"}}, Dispatch: DispatchConfig{Queues: []string{"a"}}}
	resolved, err := prepareConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	if input.Queues[0].Concurrency != 0 {
		t.Fatal("defaults mutated caller queues")
	}
	input.Queues[0].Name = "changed"
	input.Execution.Queues[0] = "changed"
	input.Dispatch.Queues[0] = "changed"
	if resolved.Queues[0].Name != "a" || resolved.Execution.Queues[0] != "a" || resolved.Dispatch.Queues[0] != "a" {
		t.Fatal("resolved config retained caller slices")
	}
}

func TestObserverTimeoutAllowsFollowingEvents(t *testing.T) {
	handled := make(chan EventKind, 2)
	bus := newEventBus(ObservabilityConfig{Buffer: 2, Logger: log.Global(), ObserverTimeout: 20 * time.Millisecond, Observer: ObserverFunc(func(ctx context.Context, event Event) {
		if event.Kind == EventStarted {
			<-ctx.Done()
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				panic("missing callback deadline")
			}
		}
		handled <- event.Kind
	})}, time.Second)
	defer bus.close()
	bus.publish(context.Background(), Event{Kind: EventStarted})
	bus.publish(context.Background(), Event{Kind: EventFinished})
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for _, want := range []EventKind{EventStarted, EventFinished} {
		select {
		case got := <-handled:
			if got != want {
				t.Fatalf("got %v want %v", got, want)
			}
		case <-timer.C:
			t.Fatal("observer blocked following event")
		}
	}
}
