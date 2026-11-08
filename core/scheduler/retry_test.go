package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestExponential(t *testing.T) {
	p := Exponential{MaxAttempts: 4, Initial: time.Second, Max: 3 * time.Second, Multiplier: 2}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	for attempt, expected := range want {
		got, retry := p.NextDelay(attempt+1, errors.New("failed"))
		if !retry || got != expected {
			t.Fatalf("attempt %d: got %v, %v", attempt+1, got, retry)
		}
	}
	if _, retry := p.NextDelay(4, errors.New("failed")); retry {
		t.Fatal("expected retry budget to be exhausted")
	}
}

func TestExponentialSaturatesDurationBoundary(t *testing.T) {
	maximum := time.Duration(1<<63 - 1)
	for _, jitter := range []bool{false, true} {
		p := Exponential{MaxAttempts: 100, Initial: maximum, Max: maximum, Multiplier: 2, Jitter: jitter}
		for attempt := 1; attempt < 100; attempt++ {
			delay, retry := p.NextDelay(attempt, errors.New("failed"))
			if !retry || delay <= 0 || delay > maximum {
				t.Fatalf("attempt=%d jitter=%v delay=%v retry=%v", attempt, jitter, delay, retry)
			}
			if !jitter && delay != maximum {
				t.Fatalf("expected exact cap, got %v", delay)
			}
		}
	}
}

type panicRetry struct{}

func (panicRetry) Identity() string {
	return "panic:v1"
}

func (panicRetry) NextDelay(int, error) (time.Duration, bool) {
	panic("broken policy")
}

func TestRetryPolicyBoundary(t *testing.T) {
	for _, cause := range []error{Permanent(errors.New("invalid payload")), errors.New("exhausted")} {
		limit := 1
		if isPermanent(cause) {
			limit = 0
		}
		delay, retry, err := evaluateRetry(panicRetry{}, 1, limit, cause)
		if delay != 0 || retry || err != nil {
			t.Fatalf("ineligible task invoked policy: %v %v %v", delay, retry, err)
		}
	}
	delay, retry, err := evaluateRetry(panicRetry{}, 1, 0, errors.New("temporary"))
	if delay != 0 || retry || !isPermanent(err) {
		t.Fatalf("policy panic: %v %v %v", delay, retry, err)
	}
}
