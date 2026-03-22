# Redis Client

基于 `redis.UniversalClient` 的 Go Redis 客户端封装，支持单机、集群和哨兵模式。

## ✨ 特性

- ✅ **统一接口**：单机/集群/哨兵模式使用相同 API
- ✅ **自动识别**：根据配置自动选择合适的模式
- ✅ **类型安全**：无需类型转换和断言
- ✅ **可观测性**：内置 OpenTelemetry Metrics、Tracing、日志支持
- ✅ **慢查询检测**：自动记录慢查询日志和告警
- ✅ **连接池管理**：完善的连接池配置和统计信息
- ✅ **优雅关闭**：安全释放资源
- ✅ **生产就绪**：完善的错误处理和重试机制
- ✅ **Hook 扩展**：支持自定义 Hooks 扩展功能

## 📦 安装

```bash
go get github.com/kochabx/kit/store/redis
```

## 🚀 快速开始

### 单机模式

```go
import (
    "context"
    "time"
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
// 认证配置
redis.WithPassword("password")       // 设置密码
redis.WithUsername("username")       // 设置用户名 (Redis 6.0+)
redis.WithDB(0)                      // 设置数据库索引（单机/哨兵模式）

// 连接池配置
redis.WithPoolSize(100)              // 设置连接池大小
```

### 可观测性

```go
// OpenTelemetry 集成
redis.WithMetrics()                              // 启用 Metrics 收集
redis.WithTracing()                              // 启用分布式追踪

// 调试和日志
redis.WithLogger(logger)                         // 设置日志记录器
redis.WithDebug()                                // 启用调试模式（记录所有命令）
redis.WithDebug(100*time.Millisecond)           // 启用调试模式并检测慢查询

// Hooks 扩展
redis.WithHooks(customHook1, customHook2)       // 添加自定义 Hooks
```

**日志级别说明**：
- `WithLogger(logger)` 设置日志记录器，记录客户端生命周期、连接错误等信息
- `WithDebug()` 启用调试模式，会记录**每个** Redis 命令的详细信息，可能产生大量日志，仅建议在调试环境使用
- `WithDebug(threshold)` 在调试模式下同时启用慢查询检测，超过阈值的查询会记录为 WARN 级别

### 集群特有选项

通过配置结构体设置：

```go
cfg := redis.Cluster("node1:6379", "node2:6379", "node3:6379")
cfg.ReadOnly = true        // 只读模式（读从节点）
cfg.RouteByLatency = true  // 按延迟路由
cfg.RouteRandomly = true   // 随机路由

client, err := redis.New(ctx, cfg, redis.WithPassword("password"))
```

## 📋 完整配置说明

通过配置结构体可以使用更多高级选项：

```go
cfg := &redis.Config{
    // 连接地址
    Addrs: []string{"localhost:6379"},
    
    // 认证信息
    Username: "default",
    Password: "password",
    DB:       0,
    
    // 协议版本 (2: RESP2, 3: RESP3)
    Protocol: 3,
    
    // 超时配置（毫秒）
    DialTimeout:  5000,
    ReadTimeout:  3000,
    WriteTimeout: 3000,
    
    // 连接池配置
    PoolSize:     100,
    MinIdleConns: 10,
    MaxIdleTime:  300000,  // 5分钟
    MaxLifetime:  0,       // 永久重用
    PoolTimeout:  4000,
    
    // 重试配置
    MaxRetries:      3,
    MinRetryBackoff: 8,
    MaxRetryBackoff: 512,
    
    // TLS 配置
    TLSConfig: tlsConfig,
    
    // 集群配置
    MaxRedirects:   3,
    ReadOnly:       false,
    RouteByLatency: false,
    RouteRandomly:  false,
}

client, err := redis.New(ctx, cfg)
```

## 🔧 完整示例

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/store/redis"
)

func main() {
    ctx := context.Background()
    logger := log.G
    
    // 生产环境配置
    cfg := redis.Cluster("node1:6379", "node2:6379", "node3:6379")
    cfg.Password = "production-password"
    cfg.PoolSize = 100
    cfg.DialTimeout = 5000
    cfg.ReadTimeout = 3000
    cfg.WriteTimeout = 3000
    
    client, err := redis.New(ctx, cfg,
        redis.WithMetrics(),                              // OpenTelemetry Metrics
        redis.WithTracing(),                              // OpenTelemetry Tracing
        redis.WithDebug(100*time.Millisecond),           // 调试 + 慢查询检测
        redis.WithLogger(logger),
    )
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // 获取底层客户端执行命令
    rc := client.UniversalClient()

    // String 操作
    rc.Set(ctx, "key", "value", time.Hour)
    val, _ := rc.Get(ctx, "key").Result()
    fmt.Println(val)

    // Hash 操作
    rc.HSet(ctx, "user:1", "name", "Alice", "age", 25)
    user, _ := rc.HGetAll(ctx, "user:1").Result()
    fmt.Println(user)

    // List 操作
    rc.LPush(ctx, "queue", "task1", "task2")
    tasks, _ := rc.LRange(ctx, "queue", 0, -1).Result()
    fmt.Println(tasks)

    // Set 操作
    rc.SAdd(ctx, "tags", "go", "redis", "cache")
    tags, _ := rc.SMembers(ctx, "tags").Result()
    fmt.Println(tags)

    // Pipeline
    pipe := rc.Pipeline()
    pipe.Set(ctx, "key1", "value1", time.Hour)
    pipe.Set(ctx, "key2", "value2", time.Hour)
    pipe.Get(ctx, "key1")
    cmds, _ := pipe.Exec(ctx)
    fmt.Printf("Executed %d commands\n", len(cmds))

    // 事务
    rc.Watch(ctx, func(tx *redis.Tx) error {
        val, err := tx.Get(ctx, "counter").Int()
        if err != nil && err != redis.Nil {
            return err
        }
        
        _, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
            pipe.Set(ctx, "counter", val+1, 0)
            return nil
        })
        return err
    })
    
    // 连接池统计
    stats := client.Stats()
    fmt.Printf("Pool stats - Total: %d, Idle: %d, Hits: %d\n",
        stats.TotalConns, stats.IdleConns, stats.Hits)
}
```

## 📊 监控

### 连接池统计

```go
stats := client.Stats()
fmt.Printf("Total connections: %d\n", stats.TotalConns)
fmt.Printf("Idle connections: %d\n", stats.IdleConns)
fmt.Printf("Pool hits: %d\n", stats.Hits)
fmt.Printf("Pool misses: %d\n", stats.Misses)
fmt.Printf("Pool timeouts: %d\n", stats.Timeouts)
fmt.Printf("Stale connections: %d\n", stats.StaleConns)
```

### OpenTelemetry 集成

客户端内置 OpenTelemetry 支持，使用 `redis/go-redis/extra/redisotel/v9` 实现：

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/trace"
)

// 初始化 OpenTelemetry
func initOTel() {
    // 配置 Tracer Provider
    tp := trace.NewTracerProvider(...)
    otel.SetTracerProvider(tp)
    
    // 配置 Meter Provider  
    mp := metric.NewMeterProvider(...)
    otel.SetMeterProvider(mp)
}

func main() {
    initOTel()
    
    // 创建客户端时自动启用追踪和指标
    client, err := redis.New(ctx, cfg,
        redis.WithTracing(),  // 启用追踪
        redis.WithMetrics(),  // 启用指标
    )
    
    // 所有 Redis 命令会自动记录 traces 和 metrics
}
```

## 🎯 最佳实践

### 1. 连接池配置

```go
import "runtime"

// 默认：10 * GOMAXPROCS
redis.WithPoolSize(10 * runtime.GOMAXPROCS(0))

// 高并发场景
redis.WithPoolSize(200)

// 低并发场景
redis.WithPoolSize(20)

// 配置最小空闲连接
cfg := redis.Single("localhost:6379")
cfg.MinIdleConns = 10  // 保持最少 10 个空闲连接
cfg.MaxIdleTime = 300000  // 空闲连接 5 分钟后关闭
```

### 2. 超时配置

```go
// 生产环境推荐配置（毫秒）
cfg := redis.Single("localhost:6379")
cfg.DialTimeout = 5000   // 连接超时 5 秒
cfg.ReadTimeout = 3000   // 读超时 3 秒
cfg.WriteTimeout = 3000  // 写超时 3 秒
cfg.PoolTimeout = 4000   // 获取连接超时 4 秒
```

### 3. 重试配置

```go
cfg := redis.Single("localhost:6379")
cfg.MaxRetries = 3              // 最多重试 3 次
cfg.MinRetryBackoff = 8         // 最小退避 8ms
cfg.MaxRetryBackoff = 512       // 最大退避 512ms

// 禁用重试
cfg.MaxRetries = -1
```

### 4. 启用可观测性

```go
// 生产环境必备
client, err := redis.New(ctx, cfg,
    redis.WithLogger(logger),                     // 设置日志记录器
    redis.WithMetrics(),                          // OpenTelemetry Metrics
    redis.WithTracing(),                          // OpenTelemetry Tracing
    redis.WithDebug(100*time.Millisecond),       // 慢查询检测（超过 100ms 告警）
)

// 调试环境（需要查看详细命令日志）
client, err := redis.New(ctx, cfg,
    redis.WithLogger(logger),
    redis.WithDebug(),  // 记录每个命令
)
```

### 4. 启用可观测性

```go
// 生产环境必备
client, err := redis.New(ctx, cfg,
    redis.WithLogger(logger),                     // 设置日志记录器
    redis.WithMetrics(),                          // OpenTelemetry Metrics
    redis.WithTracing(),                          // OpenTelemetry Tracing
    redis.WithDebug(100*time.Millisecond),       // 慢查询检测（超过 100ms 告警）
)

// 调试环境（需要查看详细命令日志）
client, err := redis.New(ctx, cfg,
    redis.WithLogger(logger),
    redis.WithDebug(),  // 记录每个命令
)
```

### 5. 优雅关闭

```go
// 确保资源释放
defer client.Close()

// 使用 context 控制生命周期
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

client, _ := redis.New(ctx, cfg)
```

### 6. 错误处理

```go
import "github.com/redis/go-redis/v9"

val, err := client.UniversalClient().Get(ctx, "key").Result()
switch {
case err == redis.Nil:
    // Key 不存在
    fmt.Println("Key does not exist")
case err != nil:
    // 其他错误（连接失败、超时等）
    fmt.Printf("Error: %v\n", err)
default:
    // 成功
    fmt.Printf("Value: %s\n", val)
}
```

### 7. 集群模式优化

```go
// 启用读写分离（读从节点）
cfg := redis.Cluster("node1:6379", "node2:6379", "node3:6379")
cfg.ReadOnly = true

// 按延迟路由（选择延迟最低的节点）
cfg.RouteByLatency = true

// 或随机路由（负载均衡）
cfg.RouteRandomly = true

client, err := redis.New(ctx, cfg, redis.WithPassword("password"))
```

### 8. 使用 Pipeline 提升性能

```go
// 批量操作使用 Pipeline
pipe := client.UniversalClient().Pipeline()

for i := 0; i < 1000; i++ {
    pipe.Set(ctx, fmt.Sprintf("key:%d", i), i, time.Hour)
}

// 一次性执行所有命令
cmds, err := pipe.Exec(ctx)
if err != nil {
    fmt.Printf("Pipeline error: %v\n", err)
}
fmt.Printf("Executed %d commands\n", len(cmds))
```

## 🔍 故障排查

### 连接失败

```go
// 测试连接
if err := client.Ping(ctx); err != nil {
    log.Printf("Connection failed: %v", err)
    
    // 检查配置
    fmt.Printf("Mode: %s\n", client.getMode())
    fmt.Printf("Addrs: %v\n", cfg.Addrs)
}

// 检查客户端状态
if client.IsClosed() {
    log.Println("Client is already closed")
}
```

### 性能问题

```go
// 1. 检查连接池状态
stats := client.Stats()
fmt.Printf("Total connections: %d\n", stats.TotalConns)
fmt.Printf("Idle connections: %d\n", stats.IdleConns)
fmt.Printf("Pool hits: %d\n", stats.Hits)
fmt.Printf("Pool misses: %d\n", stats.Misses)

// 连接池耗尽
if stats.Timeouts > 0 {
    log.Printf("Pool timeouts: %d (consider increasing PoolSize)", stats.Timeouts)
}

// 连接命中率低
hitRate := float64(stats.Hits) / float64(stats.Hits+stats.Misses)
if hitRate < 0.9 {
    log.Printf("Low hit rate: %.2f%% (consider increasing MinIdleConns)", hitRate*100)
}

// 2. 启用慢查询检测
client, err := redis.New(ctx, cfg,
    redis.WithDebug(100*time.Millisecond),  // 超过 100ms 记录警告
)

// 3. 使用 Pipeline 优化批量操作
pipe := client.UniversalClient().Pipeline()
for i := 0; i < 1000; i++ {
    pipe.Get(ctx, fmt.Sprintf("key:%d", i))
}
pipe.Exec(ctx)  // 一次性执行
```

### 高并发优化

```go
cfg := redis.Cluster("node1:6379", "node2:6379", "node3:6379")

// 增加连接池大小
cfg.PoolSize = 200
cfg.MinIdleConns = 50

// 调整超时
cfg.DialTimeout = 5000
cfg.ReadTimeout = 3000
cfg.WriteTimeout = 3000
cfg.PoolTimeout = 4000

// 集群模式优化
cfg.ReadOnly = true         // 读从节点
cfg.RouteByLatency = true  // 按延迟路由

client, err := redis.New(ctx, cfg)
```

### 内存泄漏排查

```go
// 定期检查连接数
ticker := time.NewTicker(10 * time.Second)
defer ticker.Stop()

for range ticker.C {
    stats := client.Stats()
    log.Printf("Connections - Total: %d, Idle: %d, Stale: %d",
        stats.TotalConns, stats.IdleConns, stats.StaleConns)
    
    // 异常情况告警
    if stats.TotalConns > 1000 {
        log.Printf("WARNING: Too many connections: %d", stats.TotalConns)
    }
}
```

## 📚 API 文档

### Client 方法

```go
// 核心方法
UniversalClient() redis.UniversalClient  // 获取底层客户端
Ping(ctx) error                          // 测试连接
Close() error                            // 关闭客户端并释放资源
IsClosed() bool                          // 检查客户端是否已关闭

// 统计信息
Stats() *redis.PoolStats                 // 获取连接池统计信息
```

### 配置构造方法

```go
// 创建配置
Single(addr string) *Config                           // 单机模式
Cluster(addrs ...string) *Config                      // 集群模式
Sentinel(masterName string, addrs ...string) *Config  // 哨兵模式

// 配置方法
Validate() error       // 验证配置
IsSingle() bool        // 是否单机模式
IsCluster() bool       // 是否集群模式
IsSentinel() bool      // 是否哨兵模式
```

### 配置选项

```go
// 基础选项
WithPassword(password string) Option
WithUsername(username string) Option
WithDB(db int) Option
WithPoolSize(size int) Option

// 可观测性选项
WithMetrics(opts ...redisotel.MetricsOption) Option
WithTracing(opts ...redisotel.TracingOption) Option
WithDebug(slowQueryThreshold ...time.Duration) Option
WithLogger(logger *log.Logger) Option

// Hooks 选项
WithHooks(hooks ...redis.Hook) Option
```

### 连接池统计

```go
type PoolStats struct {
    Hits        uint32  // 命中次数
    Misses      uint32  // 未命中次数  
    Timeouts    uint32  // 超时次数
    TotalConns  uint32  // 总连接数
    IdleConns   uint32  // 空闲连接数
    StaleConns  uint32  // 过期连接数
}
```

## 🔗 相关链接

- [go-redis 文档](https://redis.uptrace.dev/)
- [Redis 命令参考](https://redis.io/commands/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)