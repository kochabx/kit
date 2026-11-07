package store

import (
	"fmt"
	"github.com/redis/go-redis/v9"
)

// ValidateClient ensures operation contexts are honored by the borrowed client.
// Options must remain immutable for the scheduler's lifetime.
func ValidateClient(client redis.UniversalClient) error {
	enabled := false
	switch c := client.(type) {
	case interface{ Options() *redis.Options }:
		enabled = c.Options().ContextTimeoutEnabled
	case interface{ Options() *redis.ClusterOptions }:
		enabled = c.Options().ContextTimeoutEnabled
	case interface{ Options() *redis.RingOptions }:
		enabled = c.Options().ContextTimeoutEnabled
	default:
		return fmt.Errorf("cannot inspect Redis client timeout configuration: %w", ErrInvalidArgument)
	}
	if !enabled {
		return fmt.Errorf("Redis client requires ContextTimeoutEnabled=true: %w", ErrInvalidArgument)
	}
	return nil
}
