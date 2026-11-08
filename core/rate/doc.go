// Package rate provides Redis-backed token-bucket, fixed-window, and exact
// sliding-window limiters. Each decision is atomic and uses the Redis clock.
//
// Construct limiters with NewTokenBucket, NewFixedWindow, or NewSlidingWindow.
// Window durations must be positive whole milliseconds. Limiter instances are
// safe for concurrent use and borrow their Redis client without closing it.
package rate
