# Scheduler

`scheduler` 是一个基于 Redis 的生产级分布式任务调度模块，支持即时任务、延迟任务、Cron、失败重试、任务唯一性、协作式取消、Dead Job 与故障恢复。

## 核心语义

任务采用 **至少执行一次（at-least-once）** 语义。Worker 可能在业务副作用已经完成、但尚未记录成功状态时崩溃，因此 Handler 必须具备幂等性。

模块通过执行租约和 fencing token 防止已经失去租约的 Worker 修改任务状态。入队、派发、开始执行、完成、失败、重试和取消等状态转换均由 Lua 原子完成。

所有 Redis Key 使用相同的 Cluster hash tag，可以部署在 Redis Cluster 中。

分布式时间点与截止时间统一使用 Redis Server Time。Go 本地单调时钟仅用于 Handler 耗时、本地超时、退避等待等进程内时间计算。

即时任务通过 `HSET + XADD` 原子写入 Ready Stream，不经过 Dispatcher。只有未来任务进入 Scheduled ZSET，由 Dispatcher 以有界批次派发。

## 快速开始

```go
type Email struct {
	To string `json:"to"`
}

email := scheduler.Define[Email](
	"email.send",
	scheduler.WithTimeout(30*time.Second),
	scheduler.WithRetry(scheduler.Exponential{
		MaxAttempts: 5,
		Initial:     time.Second,
		Max:         time.Minute,
		Multiplier:  2,
		Jitter:      true,
	}),
)

s, err := scheduler.New(redisClient, scheduler.Config{
	Namespace:   "mail-production",
	Concurrency: 32,
	Role:        scheduler.RoleCombined,
})
if err != nil {
	return err
}

if err := scheduler.Handle(s, email, func(ctx context.Context, payload Email) error {
	return mailer.Send(ctx, payload.To)
}); err != nil {
	return err
}

job, err := scheduler.Enqueue(
	s,
	ctx,
	email,
	Email{To: "user@example.com"},
	scheduler.Delay(time.Minute),
	scheduler.Unique("welcome:user-123", time.Hour),
)
if err != nil {
	return err
}

// Run 会阻塞，直到 ctx 被取消或运行失败。
return s.Run(ctx)
```

仅用于生产任务的进程不需要调用 `Run`，使用完成后应调用 `s.Close()` 释放 Observer 等内部资源。

## 任务类型

### 即时任务

```go
job, err := scheduler.Enqueue(s, ctx, email, payload)
```

即时任务直接进入 Ready Stream，Worker 可以立即消费，不依赖 Dispatcher。

### 延迟任务

```go
job, err := scheduler.Enqueue(s, ctx, email, payload,
	scheduler.Delay(10*time.Minute),
)

job, err = scheduler.Enqueue(s, ctx, email, payload,
	scheduler.At(time.Date(2026, 8, 6, 9, 0, 0, 0, time.Local)),
)

// 可选：任务在队列中超过 30 分钟仍未开始执行时标记为 expired。
job, err = scheduler.Enqueue(s, ctx, email, payload,
	scheduler.ExpiresAfter(30*time.Minute),
)

// 也可以指定绝对过期时间。
job, err = scheduler.Enqueue(s, ctx, email, payload,
	scheduler.ExpiresAt(time.Now().Add(time.Hour)),
)
```

`Delay` 传递持续时间，由 Redis 计算绝对时间戳，避免节点时钟偏差。

### 唯一任务

```go
job, err := scheduler.Enqueue(s, ctx, email, payload,
	scheduler.Unique("email:user-123", time.Hour),
)
```

同一 namespace 和唯一键在有效期内只会创建一个任务。重复提交会返回已有 Job 和 `ErrDuplicate`。

Job 即使进入 `succeeded`、`dead`、`cancelled` 或 `expired`，唯一键仍按 `Unique` 调用时指定的 TTL 独立保留；任务终态不会提前解除去重窗口。

### 永久失败

当重试无法解决错误时返回：

```go
return scheduler.Permanent(err)
```

Handler panic 会被恢复并作为永久失败处理。Handler 超时会取消执行 Context，但 Go 无法强制终止忽略 Context 的业务代码。

## Cron

每次 Cron 触发都会创建一个独立 Job。多个 Dispatcher 可以同时检查同一个 Schedule，Lua CAS 保证同一次触发只创建一个 Job。

停机期间错过的执行采用固定合并语义：恢复后只生成一个补偿任务，并从 Redis 当前时间计算下一次执行时间，不回放全部历史执行。

```go
schedule, err := scheduler.ScheduleCron(
	s,
	ctx,
	email,
	Email{To: "ops@example.com"},
	"0 9 * * 1-5",
	time.UTC,
)

err = s.PauseSchedule(ctx, schedule.ID)
err = s.ResumeSchedule(ctx, schedule.ID)
err = s.UpdateSchedule(ctx, schedule.ID, "0 10 * * 1-5", time.UTC)
err = s.CancelSchedule(ctx, schedule.ID)
err = s.DeleteSchedule(ctx, schedule.ID)
```

Schedule Catalog 会保留全部状态：

```go
scheduler.ScheduleStateActive
scheduler.ScheduleStatePaused
scheduler.ScheduleStateCancelled
scheduler.ScheduleStateInvalid
```

Cancelled 和 Invalid Schedule 默认保留 `ScheduleRetention`（30 天），之后由 Dispatcher maintenance 清理。Active 和 Paused Schedule 不会自动删除。

## 生命周期

`Scheduler` 实现阻塞式 `Run(context.Context) error`。在 kit 应用容器中使用 `cx.Runner` 托管，并确保 Runner 依赖 Redis、因此先于 Redis 停止：

```go
runner := cx.MustRunner(s)
```

不要同时对同一个 Scheduler 使用 `defer s.Close()` 和 Runner 生命周期管理。`Close()` 只用于不调用 `Run` 的 producer-only Scheduler。

## 数据保留

- succeeded、cancelled、expired Job 使用 `Retention`。
- dead Job 使用 `DeadRetention`。
- `Unique` key 使用调用者传入的 TTL。
- 未配置 `ExpiresAt` 或 `ExpiresAfter` 的有效排队任务不会自动过期。
- maintenance、Dead 索引、孤儿索引和 stale consumer 清理由 `RoleDispatcher` 或 `RoleCombined` 实例执行。

可以按全局状态分页查询：

```go
paused, err := s.ListSchedules(ctx, scheduler.ScheduleQuery{
	State: scheduler.ScheduleStatePaused,
	Limit: 100,
})
```

损坏的 Cron 表达式或时区会被标记为 `invalid` 并移出活跃派发索引；孤儿索引由 Maintenance 自动修复。

## 部署角色

角色使用标志位：

```go
scheduler.RoleDispatcher
scheduler.RoleWorker
scheduler.RoleCombined
```

- `RoleDispatcher`：派发延迟任务和 Cron，并运行 Maintenance。
- `RoleWorker`：消费、执行、续租和恢复任务。
- `RoleCombined`：在同一进程中同时运行两种角色。

Dispatcher 和 Worker 可以独立部署。扩缩容时 Redis Consumer Group 会重新分配新任务；超时 Pending 消息由 `XAUTOCLAIM` 恢复。

同一 namespace 下的所有 Worker 必须注册一致的 Definition。每个 Job 都会持久化 Definition 指纹，覆盖任务名、超时、Codec 类型和重试配置；不匹配的 Worker 会拒绝执行并记录 `ErrDefinitionMismatch`，避免静默使用不同的重试语义。

## 取消与优雅关闭

```go
err := s.Cancel(ctx, job.ID)
```

Scheduled、Ready 和 Running 状态均支持取消：

- 同一 Scheduler 实例中的运行任务会被立即通知。
- 远程 Worker 最迟在 `CancellationCheckInterval` 内发现取消请求。
- Ready 状态的无效消息在消费时原子执行 `XACK + XDEL`，不会长期占用 Stream。

关闭时首先停止接收和派发新任务，在途任务仍会继续续租并等待完成。只有超过 `ShutdownTimeout` 后才会取消 Handler；任务保持可恢复状态，不会因为缩容被错误标记为 Dead。

## Dead Job 管理

```go
dead, err := s.DeadJobs(ctx, 0, 100)
err = s.RetryDead(ctx, id)
err = s.DeleteDead(ctx, id)
```

按任务类型查询时，会先在全局 Dead 集合中过滤，再应用 offset 和 limit：

```go
dead, err := s.QueryDead(ctx, scheduler.DeadQuery{
	Type:   "email.send",
	Offset: 0,
	Limit:  100,
})
```

## 统计与可观测性

```go
stats, err := s.Stats(ctx)
```

统计字段包括：

- `Scheduled`：等待未来派发的任务。
- `Ready`：尚未被 Consumer 读取的 Stream 消息。
- `Pending`：已经读取但尚未 ACK 的消息。
- `Running`：持有执行租约的任务。
- `Dead`：Dead Job 数量。
- `CronSchedules`：Catalog 中的全部 Cron Schedule。
- `ObserverDropped`：Observer 队列满时丢弃的事件数。

可以通过 `Config.Observer` 接入指标和 Trace。Observer 异步执行，其 panic 不会影响任务；`Config.Logger` 使用项目日志模块，默认值为 `log.Global()`。

## Maintenance

Maintenance 与 Dispatcher 热路径独立运行，并通过 Redis 短租约保证同一时刻只有一个 Dispatcher 执行维护工作。

其职责包括：

- 使用 Redis 时间有界清理过期 Dead 索引。
- 增量修复 Scheduled Job 和 Cron Catalog 的孤儿索引。
- 清理长时间空闲且 `Pending=0` 的 Consumer。

实体 Hash 由 Redis TTL 过期，Maintenance 只负责二级索引和 Consumer 元数据。它不会使用 `KEYS`、全量扫描或 Keyspace Notification。

相关配置：

```go
MaintenanceInterval      time.Duration
MaintenanceBatch         int64
MaintenanceDrainLimit    int64
MaintenanceLeaseDuration time.Duration
ConsumerIdleTimeout      time.Duration
```

## 关键配置

```go
scheduler.Config{
	Namespace:                 "production",
	Role:                      scheduler.RoleCombined,
	Concurrency:               16,
	DispatchBatch:             100,
	DispatchDrainLimit:        2000,
	CronDrainLimit:            2000,
	DispatchInterval:          250 * time.Millisecond,
	PollTimeout:               2 * time.Second,
	LeaseDuration:             30 * time.Second,
	LeaseRenewInterval:        time.Second,
	CancellationCheckInterval: 500 * time.Millisecond,
	ShutdownTimeout:           30 * time.Second,
	Retention:                 24 * time.Hour,
	DeadRetention:             7 * 24 * time.Hour,
	MaxPayloadBytes:           1 << 20,
}
```

配置默认值由 `default` tag 注入，约束由 `validate` tag 校验。Namespace 只允许字母、数字、点、下划线和连字符，长度不超过 128。

Redis 操作故障使用 `github.com/cenkalti/backoff/v5` 提供的指数退避和 jitter；操作恢复成功后重置退避状态。

## 生产建议

- Handler 必须幂等，推荐使用业务唯一键、事务 Inbox/Outbox 或数据库唯一约束。
- 每个环境使用独立 namespace。
- 根据任务可靠性要求配置 Redis 高可用与持久化。
- 保持 Payload 较小，默认上限为 1 MiB。
- 至少部署两个 Worker 实例，才能在节点故障后自动恢复任务。
- 将 `Ping` 接入 readiness，将 `Stats` 接入监控系统。
- 监控 Ready、Pending、Running、Dead、ObserverDropped 和任务执行延迟。

## 测试

本地 Redis 默认地址为 `localhost:6379`，默认密码为 `12345678`。可以通过环境变量覆盖：

```bash
REDIS_ADDR=localhost:6379 \
REDIS_PASSWORD=12345678 \
SCHEDULER_INTEGRATION_REQUIRED=1 \
go test -race ./core/scheduler/...
```

性能测试：

```bash
go test -run '^$' -bench '^BenchmarkBatchEnqueue$' ./core/scheduler/...
```
