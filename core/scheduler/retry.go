package scheduler

import (
	"math"
	"math/rand/v2"
	"time"

	cbackoff "github.com/cenkalti/backoff/v5"
)

type RetryPolicy interface {
	NextDelay(attempt int, err error) (time.Duration, bool)
}

type Exponential struct {
	MaxAttempts int
	Initial     time.Duration
	Max         time.Duration
	Multiplier  float64
	Jitter      bool
}

func (p Exponential) NextDelay(attempt int, _ error) (time.Duration, bool) {
	if p.MaxAttempts <= 0 || attempt >= p.MaxAttempts {
		return 0, false
	}
	initial := p.Initial
	if initial <= 0 {
		initial = time.Second
	}
	maximum := p.Max
	if maximum <= 0 {
		maximum = time.Hour
	}
	multiplier := p.Multiplier
	if multiplier < 1 {
		multiplier = 2
	}
	d := math.Min(float64(maximum), float64(initial)*math.Pow(multiplier, float64(max(0, attempt-1))))
	if p.Jitter {
		d *= 0.5 + rand.Float64()*0.5
	}
	return time.Duration(d), true
}

type NoRetry struct{}

func (NoRetry) NextDelay(int, error) (time.Duration, bool) { return 0, false }

type failureBackoff struct {
	policy *cbackoff.ExponentialBackOff
}

func newFailureBackoff(initial, maximum time.Duration) *failureBackoff {
	policy := cbackoff.NewExponentialBackOff()
	policy.InitialInterval = initial
	policy.MaxInterval = maximum
	policy.Multiplier = 2
	policy.RandomizationFactor = 0.5
	policy.Reset()
	return &failureBackoff{policy: policy}
}

func (b *failureBackoff) Reset()              { b.policy.Reset() }
func (b *failureBackoff) Next() time.Duration { return b.policy.NextBackOff() }
