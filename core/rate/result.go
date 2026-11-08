package rate

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func runScript(
	ctx context.Context,
	client redis.UniversalClient,
	script *redis.Script,
	algorithm, key string,
	limit int64,
	args ...any,
) (Result, error) {
	command := script.Run(ctx, client, []string{key}, args...)
	if err := command.Err(); err != nil {
		return Result{}, fmt.Errorf("rate: %s script: %w", algorithm, err)
	}
	raw, err := command.Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s response: %w", ErrInvalidResult, algorithm, err)
	}
	result, err := decodeResult(raw, limit)
	if err != nil {
		return Result{}, fmt.Errorf("rate: %s response: %w", algorithm, err)
	}
	return result, nil
}

func decodeResult(raw []int64, limit int64) (Result, error) {
	if len(raw) != 5 {
		return Result{}, fmt.Errorf("%w: expected five fields, got %d", ErrInvalidResult, len(raw))
	}
	if (raw[0] != 0 && raw[0] != 1) ||
		raw[1] != limit || limit <= 0 || limit > maxExactLuaInteger ||
		raw[2] < 0 || raw[2] > limit ||
		raw[3] < 0 || raw[3] > maxDurationMillis ||
		raw[4] <= 0 || raw[4] > maxExactLuaInteger {
		return Result{}, fmt.Errorf("%w: field outside valid range", ErrInvalidResult)
	}
	allowed := raw[0] == 1
	if (allowed && raw[3] != 0) || (!allowed && raw[3] == 0) {
		return Result{}, fmt.Errorf("%w: retry delay disagrees with decision", ErrInvalidResult)
	}
	return Result{
		Allowed:    allowed,
		Limit:      raw[1],
		Remaining:  raw[2],
		RetryAfter: time.Duration(raw[3]) * time.Millisecond,
		ResetAt:    time.UnixMilli(raw[4]),
	}, nil
}
