# x/rate

统一接口的限流器，提供基于 Redis GCRA（`go-redis/redis_rate/v10`）的 O(1) 分布式限流和基于 `golang.org/x/time/rate` 的有界本地限流。

## 快速开始

```go
import "github.com/kochabx/kit/x/rate"

// 1. 本地有界令牌桶（自动按 IdleTTL 惰性淘汰并限制 MaxEntries）
local := rate.NewLocalLimiter(
	rate.WithMaxEntries(10000),
	rate.WithIdleTTL(10*time.Minute),
)

res, err := local.Allow(ctx, "user:42", rate.PerSecond(20))
if err != nil {
	return err
}
if !res.Allowed {
	// 可结合 res.RetryAfter / res.ResetAfter 返回 429
}
```

```go
// 2. Redis O(1) GCRA 分布式限流（接收 goredis.UniversalClient）
redisLimiter, err := rate.NewRedisLimiter(rdb.Client(), rate.WithRedisPrefix("api:"))
if err != nil {
	return err
}
res, err := redisLimiter.AllowN(ctx, "ip:1.2.3.4", rate.Limit{
	Rate:   100,
	Burst:  150,
	Period: time.Minute,
}, 5)
```

## API

- `Limiter` 接口：`Allow(ctx, key, limit)`、`AllowN(ctx, key, limit, n)`（支持每个请求传入不同 `Limit`）；
- 配额辅助：`PerSecond(n)`、`PerMinute(n)`、`PerHour(n)`（默认 `Burst == Rate`）；
- 返回结构 `Result`：包含 `Allowed`、`Limit`、`Remaining`、`RetryAfter`、`ResetAfter`。

## 验证

```bash
go test -race -count=1 ./x/rate/...
```
