package redis

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrNil reports that a Redis key does not exist.
	ErrNil = redis.Nil
	// ErrInvalidConfig reports invalid or inconsistent Config values.
	ErrInvalidConfig = errors.New("invalid config")
	// ErrInvalidOption reports a nil or invalid functional Option.
	ErrInvalidOption = errors.New("invalid option")
)
