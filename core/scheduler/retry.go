package scheduler

import (
	"fmt"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/model"
)

type RetryPolicy interface {
	NextDelay(attempt int, err error) (time.Duration, bool)
}

type Exponential = model.Exponential

type NoRetry struct{}

func (NoRetry) NextDelay(int, error) (time.Duration, bool) { return 0, false }

func attemptLimit(policy RetryPolicy) int {
	switch p := policy.(type) {
	case Exponential:
		return max(1, p.MaxAttempts)
	case *Exponential:
		return max(1, p.MaxAttempts)
	case NoRetry, *NoRetry:
		return 1
	default:
		return 0
	}
}

// evaluateRetry is the extension boundary for user supplied retry policies.
func evaluateRetry(policy RetryPolicy, attempt, limit int, cause error) (delay time.Duration, retry bool, err error) {
	if isPermanent(cause) || (limit > 0 && attempt >= limit) {
		return 0, false, nil
	}
	defer func() {
		if value := recover(); value != nil {
			delay, retry = 0, false
			err = Permanent(fmt.Errorf("retry policy panic: %v", value))
		}
	}()
	delay, retry = policy.NextDelay(attempt, cause)
	return max(0, delay), retry, nil
}
