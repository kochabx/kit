package rate

import "errors"

// Sentinel errors identify construction, request, and response failures.
var (
	ErrNilClient     = errors.New("rate: redis client is nil")
	ErrNilContext    = errors.New("rate: context is nil")
	ErrInvalidKey    = errors.New("rate: key must not be empty")
	ErrInvalidN      = errors.New("rate: n must be positive")
	ErrNTooLarge     = errors.New("rate: n exceeds limiter capacity")
	ErrInvalidConfig = errors.New("rate: invalid limiter configuration")
	ErrInvalidOption = errors.New("rate: invalid limiter option")
	ErrInvalidResult = errors.New("rate: invalid Redis script result")
)
