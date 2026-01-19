# Redis Client

基于 `redis.UniversalClient` 的 Go Redis 客户端封装，支持单机、集群和哨兵模式。

## ✨ 特性

- ✅ **统一接口**：单机/集群/哨兵模式使用相同 API
- ✅ **自动识别**：根据配置自动选择合适的模式
- ✅ **类型安全**：无需类型转换和断言
- ✅ **可观测性**：内置 Metrics、Tracing、日志支持
- ✅ **健康检查**：定期探活和状态监控
- ✅ **慢查询检测**：自动记录慢查询
- ✅ **连接池管理**：连接池预热、统计信息
- ✅ **优雅关闭**：安全释放资源
- ✅ **生产就绪**：完善的错误处理和重试机制

## 📦 安装

```bash
go get github.com/kochabx/kit/store/redis
```

## 🚀 快速开始

### 单机模式

```go
import (
    "context"
    "github.com/kochabx/kit/store/redis"
)

ctx := context.Background()

// 创建客户端
client, err := redis.New(ctx, redis.Single("localhost:6379"),
    redis.WithPassword("mypassword"),
    redis.WithDB(0),
)
if err != nil {
    panic(err)
}
defer client.Close()

// 使用客户端
err = client.UniversalClient().Set(ctx, "key", "value", time.Hour).Err()
val, err := client.UniversalClient().Get(ctx, "key").Result()
```

### 集群模式

```go
client, err := redis.New(ctx,
    redis.Cluster("node1:6379", "node2:6379", "node3:6379"),
    redis.WithPassword("mypassword"),
    redis.WithReadOnly(), // 启用只读模式
)
```

### 哨兵模式

```go
client, err := redis.New(ctx,
    redis.Sentinel("mymaster",
        "sentinel1:26379",
        "sentinel2:26379",
        "sentinel3:26379",
    ),
    redis.WithPassword("mypassword"),
    redis.WithDB(0),
)
```

## 📖 配置选项

### 基础配置

```go
redis.WithPassword("password")       // 设置密码
redis.WithUsername("username")       // 设置用户名 (Redis 6.0+)
redis.WithDB(0)                      // 设置数据库索引（单机/哨兵）
redis.WithPoolSize(100)              // 设置连接池大小
redis.WithTimeout(5*s, 3*s, 3*s)    // 设置超时（连接/读/写）
redis.WithTLS(tlsConfig)             // 启用 TLS
```

### 可观测性

```go
redis.WithMetrics("myapp")                    // 启用 Metrics 收集
redis.WithTracing("myservice")                // 启用分布式追踪
redis.WithSlowQueryLog(100*time.Millisecond) // 启用慢查询日志
redis.WithLogger(logger)                      // 设置日志记录器
redis.WithLogging()                           // 启用命令级详细日志（需配合 WithLogger）
```

**日志级别说明**：
- `WithLogger(logger)` 设置日志记录器，记录以下信息：
  - **DEBUG** 级别：客户端创建、关闭、连接池预热等生命周期日志
  - **INFO** 级别：默认不输出客户端生命周期日志（已调整为 DEBUG）
  - **WARN** 级别：高超时率告警、慢查询告警等
  - **ERROR** 级别：连接失败、命令错误等
- `WithLogging()` 会记录**每个** Redis 命令的详细信息，可能产生大量日志，仅建议在调试环境使用

### 健康检查

```go
redis.WithHealthCheck(30*time.Second)  // 每 30 秒检查一次
```

### 连接池

```go
redis.WithPoolWarmup(10)  // 预热 10 个连接
```

### 集群选项

```go
redis.WithReadOnly()           // 只读模式（读从节点）
redis.WithRouteByLatency()     // 按延迟路由
redis.WithRouteRandomly()      // 随机路由
```

## 🔧 完整示例

```go
// 生产环境配置
client, err := redis.New(ctx,
    redis.Cluster("node1:6379", "node2:6379", "node3:6379"),
    redis.WithPassword("production-password"),
    redis.WithPoolSize(100),
    redis.WithTimeout(5*time.Second, 3*time.Second, 3*time.Second),
    redis.WithMetrics("myapp"),
    redis.WithTracing("myservice"),
    redis.WithSlowQueryLog(100*time.Millisecond),
    redis.WithHealthCheck(30*time.Second),
    redis.WithPoolWarmup(20),
    redis.WithLogger(logger),
    // redis.WithLogging(), // 仅调试时启用，会记录每个命令
)
if err != nil {
    panic(err)
}
defer client.Close()

// 获取底层客户端执行命令
rc := client.UniversalClient()

// String 操作
rc.Set(ctx, "key", "value", time.Hour)
rc.Get(ctx, "key")

// Hash 操作
rc.HSet(ctx, "user:1", "name", "Alice")
rc.HGetAll(ctx, "user:1")

// List 操作
rc.LPush(ctx, "queue", "task1")
rc.LRange(ctx, "queue", 0, -1)

// Set 操作
rc.SAdd(ctx, "tags", "go", "redis")
rc.SMembers(ctx, "tags")

// Pipeline
pipe := rc.Pipeline()
pipe.Set(ctx, "key1", "value1", time.Hour)
pipe.Set(ctx, "key2", "value2", time.Hour)
pipe.Exec(ctx)

// 事务
rc.Watch(ctx, func(tx *redis.Tx) error {
    _, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
        pipe.Set(ctx, "key", "value", time.Hour)
        return nil
    })
    return err
})
```

## 📊 监控

### 获取 Metrics

```go
metrics := client.GetMetrics()
fmt.Printf("Total commands: %d\n", metrics.CommandTotal)
fmt.Printf("Success: %d\n", metrics.CommandSuccess)
fmt.Printf("Errors: %d\n", metrics.CommandErrors)
fmt.Printf("Avg duration: %v\n", metrics.AvgDuration)
fmt.Printf("Slow queries: %d\n", metrics.SlowQueryCount)
```

### 健康状态

```go
status := client.GetHealthStatus()
fmt.Printf("Healthy: %v\n", status.Healthy)
fmt.Printf("Latency: %v\n", status.Latency)
fmt.Printf("Last check: %v\n", status.LastCheck)
```

### 连接池统计

```go
stats := client.Stats()
fmt.Printf("Total: %d\n", stats.TotalConns)
fmt.Printf("Idle: %d\n", stats.IdleConns)
fmt.Printf("Hits: %d\n", stats.Hits)
fmt.Printf("Misses: %d\n", stats.Misses)
fmt.Printf("Timeouts: %d\n", stats.Timeouts)
```

## 🎯 最佳实践

### 1. 连接池配置

```go
// 根据业务负载调整
redis.WithPoolSize(10 * runtime.GOMAXPROCS(0))  // 默认
redis.WithPoolSize(100)                          // 高并发
redis.WithPoolSize(20)                           // 低并发
```

### 2. 超时配置

```go
// 推荐配置
redis.WithTimeout(
    5*time.Second,  // 连接超时
    3*time.Second,  // 读超时
    3*time.Second,  // 写超时
)
```

### 3. 启用可观测性

```go
// 生产环境必备
redis.WithLogger(logger)                      // 设置日志记录器（生命周期日志为 DEBUG 级别）
redis.WithMetrics("myapp")                    // 启用 Metrics
redis.WithTracing("myservice")                // 启用追踪
redis.WithSlowQueryLog(100*time.Millisecond) // 慢查询检测（WARN 级别）
redis.WithHealthCheck(30*time.Second)        // 健康检查
// 不建议在生产环境启用 WithLogging()，会记录每个命令

// 调试环境（需要查看详细生命周期）
redis.WithLogger(logger)  // 设置 logger 日志级别为 DEBUG 可查看客户端创建/关闭/预热等日志
redis.WithLogging()       // 启用每个命令的详细日志
```

### 4. 优雅关闭

```go
// 确保资源释放
defer client.Close()

// 或使用 context
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

client, _ := redis.New(ctx, config)
```

### 5. 错误处理

```go
import "github.com/redis/go-redis/v9"

val, err := client.UniversalClient().Get(ctx, "key").Result()
switch {
case err == redis.Nil:
    // Key 不存在
case err != nil:
    // 其他错误
default:
    // 成功
}
```

## 🔍 故障排查

### 连接失败

```go
// 检查配置
err := client.Ping(ctx)
if err != nil {
    log.Printf("Connection failed: %v", err)
}

// 检查健康状态
status := client.GetHealthStatus()
if !status.Healthy {
    log.Printf("Unhealthy: %s", status.ErrorMessage)
}
```

### 性能问题

```go
// 检查慢查询
metrics := client.GetMetrics()
if metrics.SlowQueryCount > 0 {
    log.Printf("Detected %d slow queries", metrics.SlowQueryCount)
}

// 检查连接池
stats := client.Stats()
if stats.Timeouts > 0 {
    log.Printf("Pool timeouts: %d", stats.Timeouts)
}
```

### 高并发优化

```go
// 增加连接池大小
redis.WithPoolSize(200)

// 启用连接池预热
redis.WithPoolWarmup(50)

// 集群模式启用读写分离
redis.WithReadOnly()
```

## 📚 API 文档

### Client 方法

- `UniversalClient() redis.UniversalClient` - 获取底层客户端
- `Ping(ctx) error` - 测试连接
- `Close() error` - 关闭客户端
- `Stats() *redis.PoolStats` - 连接池统计
- `HealthCheck(ctx) error` - 健康检查
- `GetHealthStatus() *HealthStatus` - 获取健康状态
- `GetMetrics() *Metrics` - 获取 Metrics
- `IsClosed() bool` - 是否已关闭

### 配置方法

- `Single(addr) *Config` - 单机配置
- `Cluster(addrs...) *Config` - 集群配置
- `Sentinel(master, addrs...) *Config` - 哨兵配置
