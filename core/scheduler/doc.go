// Package scheduler provides typed, queue-isolated, at-least-once execution on
// Redis 8.10+. Define a Task, submit with Client.Enqueue, and Worker.Register
// its handler on a Worker before Run. Jobs and Schedules are Client management
// scopes. Handlers must be idempotent. The caller owns the borrowed Redis client.
package scheduler
