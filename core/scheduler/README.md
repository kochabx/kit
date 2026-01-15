# Redis分布式任务调度系统

一个基于Redis的高性能、生产级分布式任务调度系统，提供类型安全、简洁优雅的现代化 API。

## 🎯 核心特性

### 任务调度
- ✅ **纯泛型设计**：完全类型安全，编译时检查，无需手动序列化
- ✅ **延迟任务**：指定时间后执行
- ✅ **Cron任务**：周期性任务（支持标准Cron表达式）
- ✅ **立即任务**：立即执行
- ✅ **优先级队列**：高/中/低三级优先级
- ✅ **Redis Stream**：基于消费者组实现可靠消息队列

### 分布式特性
- ✅ **分布式锁**：基于Redis Lua脚本，防止任务重复执行
- ✅ **Worker租约**：自动注册、续约、故障转移
- ✅ **水平扩展**：支持动态增删Worker节点
- ✅ **任务去重**：防止重复提交相同任务
- ✅ **消息确认**：Stream ACK机制确保消息可靠处理
- ✅ **故障恢复**：自动接管超时的Pending消息

### 类型安全与灵活性
- ✅ **泛型 Handler**：`Handler[T]` 接口，直接处理强类型 Payload
- ✅ **泛型函数**：`HandlerFunc[T]` 函数式风格
- ✅ **自定义序列化**：支持 JSON、Protobuf、MsgPack 等任意序列化器
- ✅ **零额外开销**：泛型单态化，无运行时性能损失

### 可靠性
- ✅ **失败重试**：指数退避 + 随机抖动
- ✅ **死信队列**：超过重试次数的任务自动进入DLQ
- ✅ **任务超时**：自动超时控制
- ✅ **优雅关闭**：等待运行中任务完成
- ✅ **协程池**：基于 ants 的高性能协程池管理

### 保护机制
- ✅ **限流**：令牌桶算法防止过载
- ✅ **熔断**：自动熔断保护

### 可观测性
- ✅ **Prometheus指标**：任务、队列、Worker等全方位监控
- ✅ **结构化日志**：基于zerolog的高性能日志
- ✅ **健康检查**：HTTP健康检查接口

## 📦 安装

```bash
go get github.com/kochabx/kit/core/scheduler
```

## 🚀 快速开始

### 1. 创建调度器

```go
package main

import (
    "context"
    "log"
    
    "github.com/kochabx/kit/core/scheduler"
)

func main() {
    // 创建调度器
    s, err := scheduler.New(
        scheduler.WithRedisAddr("localhost:6379"),
        scheduler.WithNamespace("myapp"),
        scheduler.WithWorkerCount(10),
        scheduler.WithMetrics(true),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // 启动调度器
    ctx := context.Background()
    if err := s.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer s.Shutdown(ctx)
    
    // 保持运行
    select {}
}
```

### 2. 注册任务处理器

```go
// 定义 Payload 结构
type EmailPayload struct {
    To      string `json:"to"`
    Subject string `json:"subject"`
    Body    string `json:"body"`
}

// 方式 1：实现 Handler 接口
type EmailHandler struct{}

func (h *EmailHandler) Handle(ctx context.Context, payload EmailPayload) error {
    // 直接使用强类型 payload
    log.Printf("Sending email to %s: %s", payload.To, payload.Subject)
    return sendEmail(payload.To, payload.Subject, payload.Body)
}

// 注册到 Scheduler
scheduler.Register(s.Registry(), "send_email", &EmailHandler{})

// 方式 2：使用函数式风格
type SMSPayload struct {
    Phone   string `json:"phone"`
    Message string `json:"message"`
}

scheduler.Register(s.Registry(), "send_sms", 
    scheduler.HandlerFunc[SMSPayload](
        func(ctx context.Context, payload SMSPayload) error {
            return sendSMS(payload.Phone, payload.Message)
        },
    ),
)
```

### 3. 提交任务

```go
payload := EmailPayload{
    To:      "user@example.com",
    Subject: "Welcome",
    Body:    "Welcome to our service!",
}

// 立即任务
taskID, err := scheduler.Submit(s, ctx, "send_email", payload,
    scheduler.WithPriority(scheduler.PriorityHigh),
)

// 延迟任务
taskID, err := scheduler.Submit(s, ctx, "send_email", payload,
    scheduler.WithDelay(1 * time.Hour),
    scheduler.WithTaskMaxRetry(3),
    scheduler.WithTaskTimeout(30 * time.Second),
)

// Cron 周期性任务
taskID, err := scheduler.Submit(s, ctx, "daily_report", 
    map[string]any{"type": "daily"},
    scheduler.WithCron("0 0 * * *"), // 每天0点
)

// 批量提交
emails := []EmailPayload{
    {To: "user1@example.com", Subject: "Hello 1"},
    {To: "user2@example.com", Subject: "Hello 2"},
}
taskIDs, err := scheduler.BatchSubmit(s, ctx, "send_email", emails)
```

### 4. 任务选项

```go
taskID, err := scheduler.Submit(s, ctx, "my_task", payload,
    // 优先级
    scheduler.WithPriority(scheduler.PriorityHigh),
    
    // 延迟执行
    scheduler.WithDelay(5 * time.Minute),
    // 或指定时间
    scheduler.WithScheduleAt(time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)),
    
    // Cron表达式（周期性任务）
    scheduler.WithCron("0 */6 * * *"), // 每6小时
    
    // 重试配置
    scheduler.WithTaskMaxRetry(5),
    scheduler.WithTaskTimeout(10 * time.Minute),
    
    // 去重
    scheduler.WithTaskDeduplication("order:12345:payment", 1*time.Hour),
    
    // 标签
    scheduler.WithTag("env", "production"),
    scheduler.WithTag("priority", "critical"),
)
```

## 📊 Cron表达式

支持标准5字段Cron表达式：`分 时 日 月 周`

### 示例

```
0 0 * * *       # 每天0点
*/5 * * * *     # 每5分钟
0 */2 * * *     # 每2小时
0 0 1 * *       # 每月1号0点
0 0 * * 0       # 每周日0点
0 9-17 * * 1-5  # 周一到周五，9点到17点
```

### 预定义表达式

```go
@yearly   (or @annually)  # 每年1月1日0点
@monthly                   # 每月1日0点
@weekly                    # 每周日0点
@daily    (or @midnight)   # 每天0点
@hourly                    # 每小时0分
@every5m                   # 每5分钟
@every10m                  # 每10分钟
@every15m                  # 每15分钟
@every30m                  # 每30分钟
```

## 🔧 配置选项

```go
s, err := scheduler.New(
    // Redis配置
    scheduler.WithRedisAddr("localhost:6379"),
    scheduler.WithRedisDB(0),
    scheduler.WithRedisPass("password"),
    
    // 或使用已有的 Redis 客户端
    scheduler.WithRedisClient(redisClient),
    
    // 命名空间（多环境隔离）
    scheduler.WithNamespace("production"),
    
    // Worker配置
    scheduler.WithWorkerCount(20),
    scheduler.WithWorkerConcurrency(5),  // 每个Worker的协程池大小
    scheduler.WithLeaseTTL(30 * time.Second),  // Worker租约TTL，应大于RenewInterval
    
    // 队列配置
    scheduler.WithScanInterval(1 * time.Second),
    scheduler.WithBatchSize(100),
    
    // 重试配置
    scheduler.WithMaxRetry(3),  // 默认最大重试次数
    scheduler.WithRetryStrategy(
        1*time.Second,  // baseDelay
        1*time.Hour,    // maxDelay
        2.0,            // multiplier
        true,           // jitter
    ),
    
    // 去重配置
    scheduler.WithDeduplication(true, 1*time.Hour),
    
    // 死信队列
    scheduler.WithDLQ(true, 10000),
    
    // 限流
    scheduler.WithRateLimit(true, 1000, 2000),  // enabled, rate, burst
    
    // 熔断
    scheduler.WithCircuitBreaker(true, 5, 30*time.Second),  // enabled, maxFailures, timeout
    
    // 监控
    scheduler.WithMetrics(true),
    scheduler.WithMetricsPort(9090),
    
    // 健康检查
    scheduler.WithHealth(true),
    scheduler.WithHealthPort(8080),
    
    // 日志配置（可选）
    scheduler.WithCustomLogger(customLogger),  // 使用自定义日志实例
)
```

### 日志集成

Scheduler 已集成项目的统一日志系统（基于 zerolog）。

**使用默认日志（推荐）：**
```go
import "github.com/kochabx/kit/core/scheduler"

// 默认使用项目全局日志 log.L
s, _ := scheduler.New(
    scheduler.WithRedisAddr("localhost:6379"),
)
```

**使用自定义日志实例：**
```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/core/scheduler"
    "github.com/rs/zerolog"
)

// 创建文件日志
fileLog := log.NewFile(log.Config{
    Filepath:   "/var/log/scheduler",
    Filename:   "scheduler",
    RotateMode: log.RotateModeSize,
    LumberjackConfig: log.LumberjackConfig{
        MaxSize:    100,
        MaxBackups: 10,
        MaxAge:     30,
        Compress:   true,
    },
}, log.WithLevel(zerolog.InfoLevel))

s, _ := scheduler.New(
    scheduler.WithCustomLogger(fileLog),
)
```

## 🔄 序列化器

系统默认使用 JSON 序列化器，但你可以使用自定义序列化器：

```go
// 实现 Serializer 接口
type MsgPackSerializer struct{}

func (s *MsgPackSerializer) Marshal(v any) ([]byte, error) {
    return msgpack.Marshal(v)
}

func (s *MsgPackSerializer) Unmarshal(data []byte, v any) error {
    return msgpack.Unmarshal(data, v)
}

// 全局设置
s.SetSerializer(&MsgPackSerializer{})

// 或为特定任务类型设置
scheduler.RegisterWithSerializer(s.Registry(), "task_type", handler, &MsgPackSerializer{})

// 或提交时指定
taskID, err := scheduler.SubmitWithSerializer(s, ctx, "task_type", payload, &MsgPackSerializer{})
```

## 🌐 多实例部署

### 水平扩展

Scheduler 支持在同一 namespace 下运行多个实例，实现自动扩缩容：

```go
// 实例1：2个workers
s1, _ := scheduler.New(
    scheduler.WithRedisClient(rdb),
    scheduler.WithNamespace("myapp"),
    scheduler.WithWorkerCount(2),
    scheduler.WithLeaseTTL(15*time.Second),  // 必须大于RenewInterval(10s)
)
s1.Start(ctx)

// 实例2：3个workers（扩容）
s2, _ := scheduler.New(
    scheduler.WithRedisClient(rdb),
    scheduler.WithNamespace("myapp"),         // 相同namespace
    scheduler.WithWorkerCount(3),
    scheduler.WithLeaseTTL(15*time.Second),
    scheduler.WithMetrics(false),             // 禁用metrics避免重复注册
)
s2.Start(ctx)
```

### 重要配置说明

**LeaseTTL 和 RenewInterval 的关系：**
- `RenewInterval` 默认为 10 秒（硬编码，不可配置）
- `LeaseTTL` **必须大于** `RenewInterval`，建议至少为 RenewInterval 的 1.5 倍
- 如果 `LeaseTTL` 小于等于 `RenewInterval`，Worker 会在续约前过期
- 示例：`RenewInterval=10s` 时，建议 `LeaseTTL >= 15s`

**多实例 Metrics：**
- 每个实例使用相同 namespace 会导致 Prometheus metrics 重复注册
- 解决方案：
  1. 主实例启用 metrics：`WithMetrics(true)`
  2. 其他实例禁用 metrics：`WithMetrics(false)`
  3. 或为每个实例使用不同的 metrics port

**端口冲突：**
- 默认 metrics 端口：9090
- 默认 health 端口：8080
- 多实例部署时需要指定不同端口或禁用相应服务

### 动态扩缩容

```go
// 扩容：启动新实例
scaleUp := func() {
    newInstance, _ := scheduler.New(
        scheduler.WithRedisClient(rdb),
        scheduler.WithNamespace("myapp"),
        scheduler.WithWorkerCount(5),
        scheduler.WithLeaseTTL(15*time.Second),
        scheduler.WithMetrics(false),
    )
    newInstance.Start(ctx)
}

// 缩容：优雅关闭实例
scaleDown := func(s *scheduler.Scheduler) {
    s.Shutdown(ctx)  // Workers 会自动注销，不影响其他实例
}
```

## 📈 监控指标

启用Prometheus指标后，访问 `http://localhost:9090/metrics`

### 主要指标

```
# 任务提交
scheduler_task_submitted_total{type, priority}

# 任务执行
scheduler_task_executed_total{type, status}
scheduler_task_duration_seconds{type}

# 队列状态
scheduler_queue_size{queue}

# Worker状态
scheduler_worker_count
scheduler_worker_task_total{worker_id}

# 重试统计
scheduler_task_retry_total{type, retry_count}

# 死信队列
scheduler_dead_letter_queue_size

# 限流统计
scheduler_rate_limit_rejected_total

# 熔断器状态
scheduler_circuit_breaker_state{name}
```

## 🏥 健康检查

启用健康检查后，可访问以下端点：

```bash
# 综合健康检查
curl http://localhost:8080/health

# 就绪检查（K8s readiness probe）
curl http://localhost:8080/ready

# 存活检查（K8s liveness probe）
curl http://localhost:8080/live
```

响应示例：

```json
{
  "status": "healthy",
  "timestamp": "2025-12-29T10:30:00Z",
  "checks": {
    "redis": {
      "status": "ok",
      "message": "redis connection healthy"
    },
    "workers": {
      "status": "ok",
      "message": "10 workers active"
    },
    "queues": {
      "status": "ok",
      "message": "queued: 123, running: 5"
    },
    "dlq": {
      "status": "ok",
      "message": "DLQ: 0 tasks"
    }
  }
}
```

## 🔍 查询任务

```go
// 获取任务信息
taskInfo, err := s.GetTaskInfo(ctx, taskID)
fmt.Printf("Status: %s, RetryCount: %d\n", taskInfo.Status, taskInfo.RetryCount)

// 获取队列统计
stats, err := s.GetQueueStats(ctx)
fmt.Printf("Delayed: %d, Running: %d, Workers: %d\n", 
    stats.DelayedCount, stats.RunningCount, stats.WorkerCount)
```

## ❌ 任务取消

```go
// 取消等待中的任务
err := s.CancelTask(ctx, taskID)
```

注意：只能取消 `Pending` 和 `Ready` 状态的任务，运行中的任务无法取消。

## 💀 死信队列

任务超过最大重试次数后自动进入死信队列。

```go
// 获取死信任务列表
dlqTasks, err := s.dlq.GetAll(ctx)

// 从死信队列移除
err = s.dlq.Remove(ctx, taskID)

// 清空死信队列
err = s.dlq.Clear(ctx)
```

## 🔄 重试策略

系统内置多种重试策略：

### 指数退避（默认）
```go
strategy := scheduler.NewExponentialBackoff(
    1*time.Second,   // baseDelay
    1*time.Hour,     // maxDelay
    2.0,             // multiplier
    true,            // jitter
)
```

延迟计算：`delay = min(baseDelay * multiplier^retryCount, maxDelay)`

### 固定延迟
```go
strategy := scheduler.NewFixedDelay(5 * time.Second)
```

### 线性退避
```go
strategy := scheduler.NewLinearBackoff(
    1*time.Second,   // baseDelay
    30*time.Second,  // increment
    5*time.Minute,   // maxDelay
)
```

### 自定义
```go
strategy := scheduler.NewCustomRetry([]time.Duration{
    1 * time.Second,
    5 * time.Second,
    30 * time.Second,
    1 * time.Minute,
})
```

## 🏗️ 架构设计

```
┌─────────────────────────────────────┐
│        应用层 (Your App)             │
│  ┌──────────┐  ┌──────────┐        │
│  │提交任务   │  │查询状态   │        │
│  └──────────┘  └──────────┘        │
└─────────────────────────────────────┘
              ↓
┌─────────────────────────────────────┐
│          Scheduler 调度层            │
│  • 扫描延迟队列 → 就绪队列            │
│  • Cron任务调度                      │
│  • 任务去重检查                      │
│  • Pending消息接管（故障恢复）       │
└─────────────────────────────────────┘
              ↓
┌─────────────────────────────────────┐
│          Worker 执行层               │
│  ┌──────┐  ┌──────┐  ┌──────┐      │
│  │Worker│  │Worker│  │Worker│      │
│  │  1   │  │  2   │  │  N   │      │
│  │ (协程)│ │ (协程)│  │ (协程)│     │
│  │  池   │ │  池   │  │  池   │     │
│  └──────┘  └──────┘  └──────┘      │
└─────────────────────────────────────┘
              ↓
┌─────────────────────────────────────┐
│          Redis 存储层                │
│  • Delayed Queue (ZSET)             │
│  • Ready Queue (Stream x3 + CG)     │
│    - High Priority Stream           │
│    - Normal Priority Stream         │
│    - Low Priority Stream            │
│  • Task Metadata (Hash)             │
│  • Distributed Lock (String+Lua)    │
│  • Worker Registry (Hash+TTL)       │
│  • Deduplication (String+TTL)       │
│  • Dead Letter Queue (List)         │
└─────────────────────────────────────┘
```

### 核心组件说明

**延迟队列（Delayed Queue）**
- 使用 Redis ZSET 实现
- Score 为任务执行时间戳
- 调度器定期扫描并移动到期任务到就绪队列

**就绪队列（Ready Queue）**
- 使用 Redis Stream 实现，支持消费者组（Consumer Group）
- 按优先级分为 3 个 Stream（High/Normal/Low）
- Worker 按优先级顺序消费：High → Normal → Low
- 支持消息确认（ACK）机制，未确认的消息会进入 Pending 状态
- 调度器会自动接管超时的 Pending 消息，实现故障恢复

**Worker 管理**
- 每个 Worker 使用 ants 协程池管理并发任务
- 自动注册并维持租约（Lease）
- 心跳续约机制，租约过期自动清理
- 支持优雅关闭，等待运行中任务完成

**分布式锁**
- 基于 Redis Lua 脚本实现
- 防止同一任务被多个 Worker 重复执行
- 支持锁续期和安全释放

## 📝 最佳实践

### 1. 任务幂等性
确保任务处理器是幂等的，即重复执行相同任务不会产生副作用。

### 2. 超时设置
合理设置任务超时时间，避免长时间运行的任务阻塞Worker。

### 3. 错误处理
任务处理器应该返回明确的错误，便于故障排查。

### 4. 优先级使用
合理使用任务优先级，确保重要任务优先执行。Worker 会按照 High → Normal → Low 的顺序消费任务。

### 5. Worker数量与并发
根据任务类型和系统负载调整配置：
- **Worker数量** (`WithWorkerCount`)：Worker进程数，建议 2-20 个
- **Worker并发度** (`WithWorkerConcurrency`)：每个Worker的协程池大小，建议 5-10 个
- **总并发数 = Worker数量 × Worker并发度**
- CPU密集型：总并发数 ≈ CPU核心数
- IO密集型：总并发数可适当增加（2-4倍CPU核心数）

### 6. LeaseTTL 配置
**重要：** 设置 `LeaseTTL` 时必须考虑 `RenewInterval`：
- ✅ 推荐：`LeaseTTL` >= 1.5倍 `RenewInterval`
- ✅ 默认：`RenewInterval=10s`，`LeaseTTL=30s`
- ❌ 错误：`LeaseTTL` <= `RenewInterval` 会导致 Worker 过早过期
- 💡 提示：更长的 TTL 可以提高稳定性，但会延长故障 Worker 的检测时间

### 7. 多实例部署
- 所有实例使用相同的 Redis 和 namespace
- 只在一个实例上启用 metrics，或为每个实例配置不同端口
- 为每个实例配置不同的健康检查端口（如果都需要启用）
- 使用容器编排（K8s/Docker Compose）管理实例生命周期

### 8. 任务去重
- 对于需要防重复的任务，使用 `WithTaskDeduplication` 设置去重键
- 去重窗口（TTL）应根据业务需求设置，通常 1-24 小时
- 去重键应具有唯一性，如：`order:{orderID}:payment`

### 9. 监控告警
- 配置Prometheus抓取指标
- 设置队列积压告警（延迟队列、就绪队列）
- 监控死信队列增长
- 关注任务失败率和重试次数
- 监控Worker健康状态和租约续约

### 10. Cron 任务
- Cron 任务会在每次执行成功后自动调度下一次执行
- 使用 `WithCron` 选项设置周期性任务
- 如果 Cron 任务执行失败且超过重试次数，不会自动调度下次执行
- 建议为 Cron 任务设置较低的 `MaxRetry`，避免长时间阻塞

### 11. 性能优化
- 启用对象池：系统已内置 map 对象池和 strings.Builder 池
- 合理设置 `BatchSize`，控制每次扫描延迟队列的任务数
- 调整 `ScanInterval`，平衡延迟和性能
- 使用批量提交 `BatchSubmit` 提高吞吐量

## 🧪 测试

```bash
# 运行单元测试
go test -v ./core/scheduler

# 运行指定测试
go test -v -run TestScheduler ./core/scheduler

# 测试覆盖率
go test -cover ./core/scheduler

# 生成覆盖率报告
go test -coverprofile=coverage.out ./core/scheduler
go tool cover -html=coverage.out
```

## 📚 API 参考

### Scheduler 方法

```go
// 创建和启动
func New(opts ...Option) (*Scheduler, error)
func (s *Scheduler) Start(ctx context.Context) error
func (s *Scheduler) Shutdown(ctx context.Context) error

// 注册处理器
func (s *Scheduler) Registry() *Registry
func (s *Scheduler) SetSerializer(serializer Serializer)

// 提交任务
func Submit[T any](s *Scheduler, ctx context.Context, taskType string, payload T, opts ...TaskOption) (string, error)
func SubmitWithSerializer[T any](s *Scheduler, ctx context.Context, taskType string, payload T, serializer Serializer, opts ...TaskOption) (string, error)
func BatchSubmit[T any](s *Scheduler, ctx context.Context, taskType string, payloads []T, opts ...TaskOption) ([]string, error)
func BatchSubmitWithSerializer[T any](s *Scheduler, ctx context.Context, taskType string, payloads []T, serializer Serializer, opts ...TaskOption) ([]string, error)

// 查询任务
func (s *Scheduler) GetTaskInfo(ctx context.Context, taskID string) (*TaskInfo, error)
func (s *Scheduler) GetQueueStats(ctx context.Context) (*QueueStats, error)

// 取消任务
func (s *Scheduler) CancelTask(ctx context.Context, taskID string) error
```

### Registry 方法

```go
// 注册处理器
func Register[T any](r *Registry, taskType string, handler Handler[T]) error
func RegisterWithSerializer[T any](r *Registry, taskType string, handler Handler[T], serializer Serializer) error

// 管理处理器
func (r *Registry) Unregister(taskType string)
func (r *Registry) Has(taskType string) bool
func (r *Registry) List() []string
func (r *Registry) Clear()
func (r *Registry) SetSerializer(serializer Serializer)
```

### Handler 接口

```go
// 处理器接口
type Handler[T any] interface {
    Handle(ctx context.Context, payload T) error
}

// 函数式处理器
type HandlerFunc[T any] func(ctx context.Context, payload T) error

// HandlerFunc 实现 Handler 接口
func (f HandlerFunc[T]) Handle(ctx context.Context, payload T) error
```

### 任务选项

```go
// 基本选项
func WithID(id string) TaskOption
func WithPriority(priority Priority) TaskOption
func WithScheduleAt(scheduleAt time.Time) TaskOption
func WithDelay(delay time.Duration) TaskOption

// Cron 选项
func WithCron(cron string) TaskOption

// 重试和超时
func WithTaskMaxRetry(maxRetry int) TaskOption
func WithTaskTimeout(timeout time.Duration) TaskOption

// 去重
func WithTaskDeduplication(key string, ttl time.Duration) TaskOption

// 元数据
func WithTags(tags map[string]string) TaskOption
func WithTag(key, value string) TaskOption
func WithContext(ctx map[string]any) TaskOption
func WithContextValue(key string, value any) TaskOption
```

### 配置选项

```go
// Redis 配置
func WithRedisClient(client *redis.Client) Option
func WithRedisAddr(addr string) Option
func WithRedisDB(db int) Option
func WithRedisPass(pass string) Option

// 命名空间
func WithNamespace(namespace string) Option

// Worker 配置
func WithWorkerCount(count int) Option
func WithWorkerConcurrency(concurrency int) Option
func WithLeaseTTL(ttl time.Duration) Option

// 队列配置
func WithScanInterval(interval time.Duration) Option
func WithBatchSize(size int) Option

// 重试配置
func WithMaxRetry(maxRetry int) Option
func WithRetryStrategy(baseDelay, maxDelay time.Duration, multiplier float64, jitter bool) Option

// 去重和死信队列
func WithDeduplication(enabled bool, defaultTTL time.Duration) Option
func WithDLQ(enabled bool, maxSize int) Option

// 保护机制
func WithRateLimit(enabled bool, rate, burst int) Option
func WithCircuitBreaker(enabled bool, maxFailures int, timeout time.Duration) Option

// 监控和健康检查
func WithMetrics(enabled bool) Option
func WithMetricsPort(port int) Option
func WithHealth(enabled bool) Option
func WithHealthPort(port int) Option

// 日志
func WithCustomLogger(logger *log.Logger) Option
```

### 数据结构

```go
// 任务定义
type Task struct {
    ID               string
    Type             string
    Priority         Priority
    Payload          []byte
    ScheduleAt       time.Time
    Cron             string
    MaxRetry         int
    Timeout          time.Duration
    DeduplicationKey string
    DeduplicationTTL time.Duration
    Tags             map[string]string
    Context          map[string]any
}

// 任务信息（包含执行状态）
type TaskInfo struct {
    Task
    Status        TaskStatus
    RetryCount    int
    WorkerID      string
    SubmitTime    time.Time
    StartTime     *time.Time
    FinishTime    *time.Time
    LastError     string
    ExecutionTime *time.Duration
}

// 队列统计
type QueueStats struct {
    DelayedCount  int64  // 延迟队列任务数
    HighCount     int64  // 高优先级就绪任务数
    NormalCount   int64  // 普通优先级就绪任务数
    LowCount      int64  // 低优先级就绪任务数
    RunningCount  int64  // 运行中任务数
    WorkerCount   int64  // 活跃 Worker 数
    DLQCount      int64  // 死信队列任务数
}

// 优先级
const (
    PriorityLow    Priority = 1
    PriorityNormal Priority = 5
    PriorityHigh   Priority = 10
)

// 任务状态
const (
    StatusPending   TaskStatus = "pending"
    StatusReady     TaskStatus = "ready"
    StatusRunning   TaskStatus = "running"
    StatusSuccess   TaskStatus = "success"
    StatusFailed    TaskStatus = "failed"
    StatusCancelled TaskStatus = "cancelled"
    StatusDead      TaskStatus = "dead"
)
```

### Serializer 接口

```go
// 序列化器接口
type Serializer interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
}

// 默认 JSON 序列化器
var DefaultSerializer Serializer = &JSONSerializer{}

type JSONSerializer struct{}

func (s *JSONSerializer) Marshal(v any) ([]byte, error)
func (s *JSONSerializer) Unmarshal(data []byte, v any) error
```
