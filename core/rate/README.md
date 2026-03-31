# rate

基于 Redis + Lua 的分布式限流器，提供三种算法实现，统一接口。

## 特性

- **统一接口** — 所有算法实现同一个 `Limiter` 接口，可互换使用
- **key 参数化** — 单个 Limiter 实例可服务多个 key（per-user、per-IP 等），运行时动态传入
- **显式错误返回** — 调用方自行决定 fail-open 或 fail-closed 策略
- **结构化结果** — 返回 `Result`，包含剩余配额、重试等待时间、重置时间等元信息
- **原子操作** — 所有限流逻辑封装在 Lua 脚本中，Redis 端原子执行
- **毫秒级精度** — Token Bucket 和 Sliding Window 均使用毫秒时间戳

## 算法对比

| 算法 | Redis 数据结构 | 精度 | 适用场景 |
|---|---|---|---|
| **Token Bucket** | HASH | 毫秒 | 需要平滑限流 + 允许突发的场景（API 网关） |
| **Sliding Window** | ZSET | 毫秒 | 需要精确窗口统计的场景（计费、配额） |
| **Fixed Window** | STRING (INCR) | 秒 | 简单场景，最轻量（窗口边界处可能出现 2x 突发） |

## 接口

```go
type Limiter interface {
    Allow(ctx context.Context, key string, n int) (Result, error)
}

type Result struct {
    Allowed    bool          // 是否放行
    Remaining  int64         // 剩余配额
    Limit      int64         // 总配额上限
    RetryAfter time.Duration // 被拒绝时建议的重试等待时间
    ResetAt    time.Time     // 配额重置时间点
}
```

## 使用示例

### Token Bucket

令牌桶：以固定速率填充令牌，支持突发流量消耗已积累的令牌。

```go
// 桶容量 100，每秒填充 10 个令牌
limiter := rate.NewTokenBucketLimiter(redisClient, 100, 10)

result, err := limiter.Allow(ctx, "user:123", 1)
if err != nil {
    // Redis 故障，自行决定策略
    log.Warn().Err(err).Msg("rate limiter error")
    // fail-open: 放行
    // fail-closed: 拒绝
}
if !result.Allowed {
    // 被限流，可使用 result.RetryAfter 告知客户端
    fmt.Printf("rate limited, retry after %v\n", result.RetryAfter)
}
```

### Sliding Window

滑动窗口：在滑动的时间窗口内精确计数，无边界突发问题。

```go
// 10 秒窗口，最多 100 个请求
limiter := rate.NewSlidingWindowLimiter(redisClient, 10*time.Second, 100)

result, err := limiter.Allow(ctx, "ip:10.0.0.1", 1)
```

### Fixed Window

固定窗口：最简单的计数器方案，窗口到期后自动重置。

```go
// 1 分钟窗口，最多 60 个请求
limiter := rate.NewFixedWindowLimiter(redisClient, time.Minute, 60)

result, err := limiter.Allow(ctx, "api:/v1/orders", 1)
```

### 批量请求

所有限流器都支持一次请求多个配额：

```go
// 一次请求 5 个配额
result, err := limiter.Allow(ctx, "user:123", 5)
```

## 错误处理

`Allow` 返回 `error` 而非静默失败，典型策略：

```go
result, err := limiter.Allow(ctx, key, 1)
if err != nil {
    // fail-open: Redis 故障时放行
    return true
}
return result.Allowed
```

## 实现细节

### Token Bucket

- 使用单个 HASH key 存储 `tokens`（当前令牌数）和 `ts`（上次更新时间戳）
- 每次请求时根据经过时间补充令牌：`filled = min(capacity, tokens + elapsed_ms * rate / 1000)`
- TTL 自动设为填满时间的 2 倍，避免无用 key 堆积

### Sliding Window

- 使用 ZSET，score 为毫秒时间戳，member 为唯一标识
- 每次请求：`ZREMRANGEBYSCORE` 清理过期 → `ZCARD` 计数 → 条件 `ZADD`
- 操作复杂度 O(log n)，适合高流量场景

### Fixed Window

- 使用 `INCR` + `EXPIRE`，最少的 Redis 命令开销
- 首次写入时设置窗口过期时间
- 注意：窗口边界处可能出现最多 2x 的瞬时流量
