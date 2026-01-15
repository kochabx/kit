package rate

import (
	"context"
	"time"
)

type Limiter interface {
	Allow() bool
	AllowN(t time.Time, n int) bool
	AllowNCtx(ctx context.Context, t time.Time, n int) bool
}
