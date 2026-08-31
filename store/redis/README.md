# Redis

`store/redis` 是基于 go-redis UniversalClient 的轻量封装，支持单机、Redis Cluster 和 Sentinel。模块负责配置默认值与校验、客户端类型选择、OpenTelemetry instrumentation、调试日志以及应用生命周期集成。

## 快速开始

```go
package main

import (
	"context"
	"log"
	"time"

	kitredis "github.com/kochabx/kit/store/redis"
)

func main() {
	cfg := kitredis.Single("127.0.0.1:6379")
	cfg.Password = "secret"
	cfg.DB = 1

	client, err := kitredis.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		log.Fatal(err)
	}

	err = client.UniversalClient().Set(ctx, "key", "value", time.Hour).Err()
	if err != nil {
		log.Fatal(err)
	}
}
```

`New` 只创建惰性客户端，不会主动连接 Redis。使用 `Ping` 验证连接，或者把 Client 注册为应用组件，由 `Start` 完成检查。

## 客户端模式

### 单机

```go
cfg := redis.Single("redis.internal:6379")
client, err := redis.New(cfg)
```

### Redis Cluster

```go
cfg := redis.Cluster(
	"redis-1.internal:6379",
	"redis-2.internal:6379",
	"redis-3.internal:6379",
)
cfg.ReadOnly = true
cfg.RouteByLatency = true

client, err := redis.New(cfg)
```

`Cluster` 会显式启用 cluster mode，因此也支持只有一个 configuration endpoint 的托管集群：

```go
cfg := redis.Cluster("cluster.internal:6379")
```

`Config.Mode` 使用类型安全的 `ModeSingle`、`ModeCluster` 和 `ModeSentinel`。通常应使用上面的构造函数设置它，而不是根据地址数量猜测客户端模式。

### Sentinel

```go
cfg := redis.Sentinel(
	"mymaster",
	"sentinel-1.internal:26379",
	"sentinel-2.internal:26379",
)
cfg.Password = "secret"

client, err := redis.New(cfg)
```

## 配置

连接、认证、超时、连接池和重试都通过 `Config` 表达，不再提供重复的 functional option。

```go
cfg := redis.Single("redis.internal:6379")
cfg.Username = "app"
cfg.Password = "secret"
cfg.DB = 2
cfg.Protocol = 3

cfg.DialTimeout = 5 * time.Second
cfg.ReadTimeout = 3 * time.Second
cfg.WriteTimeout = 3 * time.Second

cfg.PoolSize = 100
cfg.MinIdleConns = 10
cfg.MaxIdleTime = 5 * time.Minute
cfg.MaxLifetime = time.Hour
cfg.PoolTimeout = 4 * time.Second

cfg.MaxRetries = 3
cfg.MinRetryBackoff = 8 * time.Millisecond
cfg.MaxRetryBackoff = 512 * time.Millisecond
```

主要默认值：

| 字段 | 默认值 |
| --- | --- |
| `Addrs` | `localhost:6379` |
| `Protocol` | `3` |
| `DialTimeout` | `5s` |
| `ReadTimeout` | `3s` |
| `WriteTimeout` | `3s` |
| `MaxIdleTime` | `5m` |
| `PoolTimeout` | `4s` |
| `MinRetryBackoff` | `8ms` |
| `MaxRetryBackoff` | `512ms` |
| `MaxRedirects` | `3` |

`PoolSize == 0` 时由 go-redis 使用自身默认值。`MaxRetries == -1` 表示禁用重试；零值使用 go-redis 的默认重试行为。

`New` 会复制配置后再应用默认值，不会修改调用方传入的 `Config` 或 `Addrs`。

### TLS

```go
cfg.TLSConfig = &tls.Config{
	MinVersion: tls.VersionTLS12,
	ServerName: "redis.internal",
}
```

## Runtime Options

Functional Option 只用于运行时依赖和 instrumentation：

### 日志与慢命令

```go
client, err := redis.New(cfg,
	redis.WithLogger(log.Global()),
	redis.WithDebug(200*time.Millisecond),
)
```

- 默认不开启命令日志。
- `WithLogger` 设置生命周期和调试日志的输出目标。
- `WithDebug()` 记录连接和命令执行结果。
- `WithDebug(threshold)` 同时标记超过阈值的慢命令。
- 单独使用 `WithDebug` 时使用 `log.Global()`。
- 日志只记录命令名称，不记录参数，避免泄漏密码、token 或业务数据。

调试模式会为每条 Redis 命令产生日志，不建议在高流量生产环境长期启用。

### OpenTelemetry

```go
client, err := redis.New(cfg,
	redis.WithTracing(),
	redis.WithMetrics(),
)
```

两个 Option 都使用 go-redis 官方 `redisotel` instrumentation，并可传入对应的原生 Option。

### 自定义 Hook

```go
client, err := redis.New(cfg,
	redis.WithHooks(hookA, hookB),
)
```

nil Option、nil logger、nil hook 和非法慢命令阈值会返回 `ErrInvalidOption`。

## Client API

```go
underlying := client.UniversalClient()
stats := client.Stats()

if err := client.Ping(ctx); err != nil {
	return err
}
if err := client.Close(); err != nil {
	return err
}
```

`UniversalClient()` 是主要业务入口，返回 go-redis 的 `redis.UniversalClient`，可以执行命令、事务、pipeline 和 Lua script。

Client 同时实现应用组件生命周期方法：

- `Start(ctx)`：执行 Ping
- `HealthCheck(ctx)`：执行 Ping
- `Stop(ctx)`：关闭底层客户端

如果 Client 由应用组件管理，不要再对同一个实例执行 `defer client.Close()`。

## 错误处理

```go
client, err := redis.New(cfg, options...)
switch {
case errors.Is(err, redis.ErrInvalidConfig):
	// 配置错误
case errors.Is(err, redis.ErrInvalidOption):
	// Option 错误
case err != nil:
	// instrumentation 初始化失败
}
```

Redis key 不存在时，可以继续使用：

```go
errors.Is(err, redis.ErrNil)
```

`Ping`、`Close` 和 instrumentation 错误都使用 `%w` 保留底层错误链。模块不会在错误或日志中输出密码及完整命令参数。

## JSON

所有普通配置字段都提供 JSON tag；`TLSConfig` 不参与 JSON 序列化。时间字段使用 `time.Duration`，通过 Go 标准 JSON 解码时对应纳秒整数。如需使用 `"5s"` 形式，应由上层配置系统提供 duration 解码能力。
