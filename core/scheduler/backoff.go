package scheduler

import (
	"time"

	cbackoff "github.com/cenkalti/backoff/v5"
)

type failureBackoff struct {
	policy  *cbackoff.ExponentialBackOff
	maximum time.Duration
}

func newFailureBackoff(initial, maximum time.Duration) *failureBackoff {
	policy := cbackoff.NewExponentialBackOff()
	policy.InitialInterval = initial
	policy.MaxInterval = maximum
	policy.Multiplier = 2
	policy.RandomizationFactor = 0.5
	policy.Reset()
	return &failureBackoff{policy: policy, maximum: maximum}
}

func (b *failureBackoff) Reset() {
	b.policy.Reset()
}

func (b *failureBackoff) NextDelay() time.Duration {
	return min(b.maximum, b.policy.NextBackOff())
}
