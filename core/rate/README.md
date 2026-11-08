# rate

基于 Redis 的分布式限流器。每次判定通过 Lua 原子执行，时间来自 Redis 服务端。限流器可以并发共享，借用 `redis.UniversalClient`，不负责关闭客户端。

## 算法

| 算法 | 数据结构 | 适用特点 |
|---|---|---|
| TokenBucket | HASH | 固定补充速率，允许突发流量 |
| FixedWindow | HASH | 按自然时间边界计数，状态较小 |
| SlidingWindow | ZSET | 精确保存带权事件，避免固定窗口边界突发 |

三种算法都实现 `Limiter.AllowN(ctx, key, n)`，具体类型另提供申请一个配额的 `Allow(ctx, key)`。

## 使用

```go
limiter, err := rate.NewTokenBucket(redisClient, 10, 100,
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
    return fmt.Errorf("retry after %s", result.RetryAfter)
}
```

令牌补充速率为每秒令牌数，可以是小数；burst 是最大容量。参数必须有限、为正，完整补充周期及返回的等待时间必须能由 `time.Duration` 表示。

固定窗口和滑动窗口使用 `time.Duration`：

```go
fixed, err := rate.NewFixedWindow(redisClient, 60, time.Minute)
sliding, err := rate.NewSlidingWindow(redisClient, 100, 10*time.Second)
precise, err := rate.NewSlidingWindow(redisClient, 5, 250*time.Millisecond)
```

窗口最小为一毫秒，必须是整毫秒，不会静默截断更小单位。配额上限为 `2^53-1`，以保持 Redis Lua 数值精度。Context 必须非 nil，`n` 必须大于零且不超过该限流器容量。

## 返回结果与错误

```go
type Result struct {
    Allowed    bool
    Remaining  int64
    Limit      int64
    RetryAfter time.Duration
    ResetAt    time.Time
}
```

`Remaining` 是判定后的剩余配额。拒绝不会扣减配额；允许时 `RetryAfter` 为零，拒绝时为正。`ResetAt` 是预计恢复全部配额的服务端时间。

输入和协议错误可通过 `errors.Is` 区分：

- `ErrNilClient`：nil 或 typed nil Redis 客户端。
- `ErrNilContext`：nil Context。
- `ErrInvalidConfig`：非法速率、容量或窗口。
- `ErrInvalidOption`：nil Option 或归一化后为空的前缀。
- `ErrInvalidKey`：空白业务 key。
- `ErrInvalidN` / `ErrNTooLarge`：非法申请量。
- `ErrInvalidResult`：Redis 返回值类型、长度、范围或判定状态不符合协议。

Redis 执行错误保留原始错误链。调用方决定故障时放行还是拒绝，不会由模块自动降级。

## Key 与共享状态

```go
limiter, err := rate.NewFixedWindow(redisClient, 60, time.Minute,
    rate.WithKeyPrefix("orders"),
)
```

前缀默认 `rate`，会移除首尾空白和冒号；业务 key 保留原始内容，仅拒绝空白值。窗口参数在内部使用毫秒，生成的 key 类似：

```text
orders:fixed-window:60:60000:user:123
```

前缀、算法或限流参数不同都会隔离状态。每次脚本只操作一个 key，可用于 Redis Cluster。不要由其他程序修改内部数据结构。

限流判定不提供业务幂等：请求结果丢失后重试可能再次消耗配额。多个进程可共享限流状态，但不应由多个 Redis 实例分别维护同一业务配额。

## 滑动窗口成本

精确滑动窗口每次读取并扫描窗口内的事件，成本随事件条数增加。每个允许的请求保存一条带权事件，不会按申请量 n 创建 n 条记录。需要更低且稳定的请求成本时，可根据业务容忍的边界突发选择固定窗口或令牌桶。

`workload_benchmark_test.go` 分别测量 32、1024、8192 条事件时的请求成本；基准使用固定占用量，不会不断扩大窗口。

## 组织与验证

公共 API 和校验在 `limiter.go`，选项和错误分别在 `options.go`、`errors.go`，脚本结果由 `result.go` 统一解码。各算法的 Go/Lua 文件继续成对维护。

单元测试不访问网络：

```bash
go test -race ./core/rate -count=1
```

集成测试位于 `integration_test.go`，未设置地址或指定 `-short` 时跳过。设置地址后，连接失败会使测试失败；认证通过环境变量提供：

```bash
RATE_REDIS_ADDR=localhost:6379 RATE_REDIS_PASSWORD=secret \
go test -race ./core/rate -count=3

RATE_REDIS_ADDR=localhost:6379 RATE_REDIS_PASSWORD=secret \
go test ./core/rate -run '^$' -bench '^BenchmarkSlidingWindowEvents$' -benchmem -benchtime=100ms
```

测试和基准使用随机前缀，只清理各自的 key，不执行 FLUSHDB/FLUSHALL。测试覆盖毫秒窗口、容量边界、令牌补充、并发扣减、Context 取消和 Key 隔离。
