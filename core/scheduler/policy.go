package scheduler

import (
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/model"
)

// ExecutionPolicy is frozen into each Job and Schedule at submission.
// Duration values use Go duration nanoseconds in the JSON protocol.
type ExecutionPolicy = model.ExecutionPolicy

type RetrySpec = model.RetrySpec

// IdentifiedRetryPolicy identifies all behavior-affecting configuration.
// Implementations must be immutable, concurrent-safe, and stable across processes.
type IdentifiedRetryPolicy interface {
	RetryPolicy
	Identity() string
}

func snapshotPolicy(timeout time.Duration, retry RetryPolicy, recoveries, executions int) (ExecutionPolicy, error) {
	p := ExecutionPolicy{Timeout: timeout, MaxRecoveries: recoveries, MaxExecutions: executions}
	switch r := retry.(type) {
	case Exponential:
		p.Retry = RetrySpec{Kind: "exponential", Exponential: r}
	case *Exponential:
		p.Retry = RetrySpec{Kind: "exponential", Exponential: *r}
	case NoRetry, *NoRetry:
		p.Retry = RetrySpec{Kind: "none"}
	case IdentifiedRetryPolicy:
		id := r.Identity()
		if id == "" {
			return p, fmt.Errorf("empty retry policy identity: %w", ErrInvalidArgument)
		}
		p.Retry = RetrySpec{Kind: "custom", Identity: id}
	default:
		return p, fmt.Errorf("custom retry policy must implement Identity: %w", ErrInvalidArgument)
	}
	if _, err := json.Marshal(p); err != nil {
		return p, fmt.Errorf("encode execution policy: %w: %w", ErrInvalidArgument, err)
	}
	return p, nil
}

func retryPolicy(p ExecutionPolicy, h handler) (RetryPolicy, error) {
	switch p.Retry.Kind {
	case "exponential":
		return p.Retry.Exponential, nil
	case "none":
		return NoRetry{}, nil
	case "custom":
		if r, ok := h.retry.(IdentifiedRetryPolicy); ok && h.retryIdentity == p.Retry.Identity {
			return r, nil
		}
	}
	return nil, ErrTaskMismatch
}

func policyAttemptLimit(p ExecutionPolicy) int {
	switch p.Retry.Kind {
	case "none":
		return 1
	case "exponential":
		return max(1, p.Retry.Exponential.MaxAttempts)
	default:
		return 0
	}
}
