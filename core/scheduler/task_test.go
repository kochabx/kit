package scheduler

import (
	"sync/atomic"
	"testing"
	"time"
)

type countingRetry struct{ calls *atomic.Int64 }

func (p countingRetry) Identity() string                           { p.calls.Add(1); return "count:v1" }
func (p countingRetry) NextDelay(int, error) (time.Duration, bool) { return 0, false }

func (p countingRetry) MarshalJSON() ([]byte, error) { p.calls.Add(1); return []byte(`{"max":1}`), nil }

func TestTaskFingerprintComputedOnce(t *testing.T) {
	calls := &atomic.Int64{}
	d := mustTask[string]("cached", WithRetry(countingRetry{calls}))
	want := d.Fingerprint()
	if want == "" {
		t.Fatal("missing fingerprint")
	}
	for range 10 {
		got := d.Fingerprint()
		if got != want {
			t.Fatal("fingerprint changed")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("retry serialized %d times", calls.Load())
	}
	_, err := NewTaskWithCodec[string]("invalid", nil)
	if err == nil {
		t.Fatal("nil codec accepted")
	}
}

func TestQueueConfigurationAndFingerprints(t *testing.T) {
	for _, configs := range [][]QueueConfig{{{Name: "a"}, {Name: "a"}}, {{Name: "invalid queue"}}, {{Name: ""}}, {{Name: "a", Concurrency: -1}}} {
		if _, err := prepareConfig(WorkerConfig{Queues: configs}); err == nil {
			t.Fatalf("invalid queue configuration accepted: %+v", configs)
		}
	}
	config, err := prepareConfig(WorkerConfig{Queues: []QueueConfig{{Name: "a", Concurrency: 2}, {Name: "b", Concurrency: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if config.Queues[0].Concurrency != 2 || config.Queues[1].Concurrency != 3 || config.Queues[0].Prefetch != 0 {
		t.Fatalf("queue defaults=%+v", config.Queues)
	}
	a := mustTask[int]("same", WithQueue("a")).Fingerprint()
	b := mustTask[int]("same", WithQueue("b")).Fingerprint()
	if a != b {
		t.Fatal("queue must not affect wire fingerprint")
	}

	if _, err := NewTask[int]("invalid", WithQueue("")); err == nil {
		t.Fatal("empty definition queue accepted")
	}
}

func TestJSONCodecV2Defaults(t *testing.T) {
	encoded, err := (jsonCodec[[]string]{}).Marshal(nil)
	if err != nil || string(encoded) != "[]" {
		t.Fatalf("nil slice encoding=%s err=%v", encoded, err)
	}
	encoded, err = (jsonCodec[map[string]string]{}).Marshal(nil)
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("nil map encoding=%s err=%v", encoded, err)
	}
	if _, err := (jsonCodec[map[string]int]{}).Unmarshal([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("duplicate object member accepted")
	}
}
