# rate

`rate` 提供基于 Redis 的分布式限流器。限流判定及结果时间均在 Redis
服务端原子计算，不受应用节点时钟偏差影响。

## 算法

| 算法 | Redis 数据结构 | 特点 |
|---|---|---|
| 令牌桶 | HASH | 按固定速率补充令牌，支持突发流量 |
| 固定窗口 | HASH | 开销较低，按自然时间边界划分窗口 |
| 滑动窗口 | ZSET | 精确记录带权事件，不存在固定窗口的边界突发 |

所有算法均实现以下接口：

```go
type Limiter interface {
    AllowN(ctx context.Context, key string, n int64) (Result, error)
}
```

`n` 表示本次申请的配额数量，必须大于零。构造函数负责校验限流参数。
Redis 错误会直接返回，由调用方决定故障时放行还是拒绝。

三个具体限流器还提供 `Allow(ctx, key)` 便捷方法，默认申请一个配额；
`Limiter` 接口仅保留不可再简化的核心能力 `AllowN`。

## 使用方式

### 令牌桶

```go
limiter, err := rate.NewTokenBucket(
    redisClient,
    10,  // 每秒补充的令牌数
    100, // 最大突发容量
    rate.WithKeyPrefix("orders"),
)
if err != nil {
    return err
}

result, err := limiter.Allow(ctx, "user:123")
if err != nil {
    return err
}
if !result.Allowed {
    return fmt.Errorf("请求被限流，请在 %s 后重试", result.RetryAfter)
}
```

### 固定窗口

```go
// 60 秒内最多允许 60 个配额
limiter, err := rate.NewFixedWindow(redisClient, 60, 60)
```

### 滑动窗口

```go
// 10 秒内最多允许 100 个配额
limiter, err := rate.NewSlidingWindow(redisClient, 100, 10)
```

## Redis key

默认 Redis key 前缀为 `rate`，可通过 `WithKeyPrefix` 修改：

```go
limiter, err := rate.NewFixedWindow(
    redisClient,
    60,
    60,
    rate.WithKeyPrefix("orders"),
)
```

Redis key 包含前缀、算法、限流参数和业务 key，例如：

```text
orders:fixed-window:60:60:user:123
```

不同算法和限流参数不会共享状态。每个 Lua 脚本只操作一个 Redis key，
可用于 Redis Cluster。

## 返回结果

```go
type Result struct {
    Allowed    bool
    Remaining  int64
    Limit      int64
    RetryAfter time.Duration
    ResetAt    time.Time
}
```

- `Allowed`：是否允许本次请求。
- `Limit`：配额上限。
- `Remaining`：判定完成后的剩余配额；拒绝的请求不会消耗配额。
- `RetryAfter`：再次申请相同数量配额前的最短等待时间；请求被允许时为零。
- `ResetAt`：当前已消耗配额预计完全恢复的时间。

## 测试

单元测试不依赖 Redis。需要运行真实 Redis 集成测试时，设置以下环境变量：

```bash
RATE_REDIS_ADDR=localhost:6379 \
RATE_REDIS_PASSWORD=secret \
go test ./core/rate -count=1
```
