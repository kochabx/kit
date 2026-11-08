# Scheduler

Redis 专用、类型安全、按队列隔离的持久化任务调度器。要求 Redis 8.10+，部署使用稳定系列已验证的补丁版本。数据协议从 v1 开始，不兼容旧方案。完整设计及验收记录见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 概念与入口

- `Task[T]` 是不可变定义：名称、版本、Codec、Queue 和默认执行策略。
- `Client` 提交与管理任务，不启动执行池。
- `Worker` 注册 Handler，负责执行、派发、续租、恢复和清理。
- `client.Jobs()` 管理任务实例；`client.Schedules()` 管理周期规则；`client.Queue(name)` 提供队列查询。

Task 是定义，Job 是一次提交，Schedule 按周期创建独立 Job。没有公开的 Scheduler 或 TaskClient，也无需 Bind。

## 使用

```go
// redisClient 由应用持有，构造时设置 ContextTimeoutEnabled:true。
// Client 和 Worker 不关闭它。
client, err := scheduler.NewClient(redisClient, scheduler.ClientConfig{
 Namespace: "app",
 Queues: []string{"mail"},
})
if err != nil { return err }
defer client.Close()

worker, err := scheduler.NewWorker(redisClient, scheduler.WorkerConfig{
 Namespace: "app",
 Queues: []scheduler.QueueConfig{{Name: "mail", Concurrency: 16}},
})
if err != nil { return err }
defer worker.Close() // 释放未运行 Worker；运行后由 Run 完成资源清理

type Email struct { To string `json:"to"` }
email, err := scheduler.NewTask[Email]("email.send",
 scheduler.WithQueue("mail"),
 scheduler.WithVersion("1"),
 scheduler.WithTimeout(30*time.Second),
 scheduler.WithRetry(scheduler.Exponential{
  MaxAttempts: 4, Initial: time.Second, Max: time.Minute,
  Multiplier: 2, Jitter: true,
 }),
 scheduler.WithRecoveryLimit(3),
 scheduler.WithExecutionLimit(100),
)
if err != nil { return err }
if err := worker.Register(email, func(ctx context.Context, e Email) error {
 return sendEmail(ctx, e.To) // 业务逻辑必须幂等
}); err != nil { return err }

job, err := client.Enqueue(ctx, email, Email{To:"user@example.com"},
 scheduler.WithJobID("request-123"),
)
if err != nil { return err }
_ = job

return worker.Run(ctx)
```

仅生产任务的进程只构造 Client。注册发生在 Run 前，重复注册同 Queue/Name/Version 会失败。Worker 只能运行一次。配置未声明队列时使用 default；显式队列不会自动追加 default。

## 提交与幂等

```go
job, err := client.Enqueue(ctx, email, payload, scheduler.Delay(time.Minute))
job, err = client.Enqueue(ctx, email, payload, scheduler.At(runAt))
job, err = client.Enqueue(ctx, email, payload, scheduler.ExpiresAfter(time.Hour))
results := client.EnqueueBatch(ctx, email, payloads)
```

`WithJobID` 是调用方重发同一请求的幂等标识，允许字母、数字、点、下划线和连字符，最长 128 字符。相同 ID/请求返回已有 Job 和 ErrDuplicate；实体已清理时返回 nil 和 ErrDuplicate。不同 Queue、Task、Payload、策略或时间选项复用同 ID 返回 ErrConflict。幂等窗口由 Retention.Submission 决定，默认 24 小时，窗口过后且 Job 不存在时可重新创建。重发相对 Delay/ExpiresAfter 时沿用原请求语义，不重新计算原任务时间。

`Unique(key, ttl)` 是独立的业务去重窗口，跨 Queue 共享 namespace；不是可替代 WithJobID 的请求回执。同一业务 key 的其他请求返回已有 Job 和 ErrDuplicate，实体删除不提前释放 Unique key。

批量提交按最多 256 条或 4 MiB payload 分块，保留输入顺序和逐项错误，不提供整体事务。每次提交固化执行策略；大 Payload 建议改传业务标识。

## 协议与重试

指纹包含 TaskName、Version 和 Codec.Identity，不包含 Go 类型名、Queue、Timeout 或重试配置。JSON Codec 标识为 `json:v1`（调度器内部协议标识），实现使用 `encoding/json/v2`；Payload schema 改变时显式增加 WithVersion。

自定义 Codec 必须提供稳定 Identity。内置 Exponential/NoRetry 固化为数据；自定义 RetryPolicy 必须实现 IdentifiedRetryPolicy，Identity 包含所有影响行为的配置，Worker 必须注册匹配实现。Codec 和自定义策略必须不可变且支持并发。

同一 Worker 可注册多个版本。同一 Queue 的消费节点必须支持该 Queue 中全部任务版本；不同支持集合的 Worker 使用不同 Queue，Stream 不按 Handler 版本过滤。缺少 Handler、Codec 不匹配或自定义策略不匹配会进入 Dead，不静默丢弃或使用其他策略。

`MaxAttempts` 限制业务失败次数（包含首次失败）；`WithRecoveryLimit` 单独限制租约失效后的恢复次数，默认 3；`WithExecutionLimit` 限制一个人工重试周期内的总执行次数，默认 100。NoRetry 禁止业务重试，不禁止崩溃恢复。人工 Jobs.Retry 重置计数并增加 generation，避免新一轮投递被 IDMP 去重。

## 周期任务与管理

```go
schedule, err := client.Schedules().Create(ctx, email, payload,
 scheduler.CronSpec{Expression:"0 9 * * 1-5", TimeZone:"Asia/Shanghai"},
)
err = client.Schedules().Pause(ctx, schedule.ID)
err = client.Schedules().Resume(ctx, schedule.ID)
err = client.Schedules().UpdateCron(ctx, schedule.ID,
 scheduler.CronSpec{Expression:"0 10 * * 1-5", TimeZone:"Asia/Shanghai"})
err = client.Schedules().Cancel(ctx, schedule.ID)
err = client.Schedules().Delete(ctx, schedule.ID)

current, err := client.Jobs().Get(ctx, job.ID)
err = client.Jobs().Cancel(ctx, job.ID)
err = client.Jobs().Retry(ctx, deadID)
err = client.Jobs().Delete(ctx, terminalID)
```

Schedules.Create 是 Go 1.27 泛型方法，使用 Schedules 管理作用域。TimeZone 默认 UTC，只接受 UTC/IANA 名称，不接受 Local 或表达式内 TZ/CRON_TZ。多 Dispatcher 用 CAS 防止重复创建 occurrence；停机错过的触发合并一次。暂停时更新保持暂停，取消后不可恢复。周期创建的 Job 继承创建 Schedule 时的执行策略。

Delete 只允许终态，Retry 只允许 Dead。运行中取消设置标志并通过心跳通知 Handler；不能强制终止业务 goroutine。

## 查询、指标与生命周期

Queue 提供 Stats、DeadJobs、RunningJobs、Schedules。列表使用 Cursor/Count，Count 是扫描提示；没有排序或快照保证，空页仍需继续直到 NextCursor=0，完整遍历可以按 ID 去重。

WorkerConfig.Role 控制执行/派发角色；Execution.Queues、Dispatch.Queues 为 nil 表示订阅全部声明 Queue，显式空切片表示零个。每 Queue 独立执行池和接收缓冲，容量跨实例累加。Prefetch=0 仍可能由读取批次持有未执行消息，FetchBatch 限制条数，ReadMaxBytes 限制 Stream 响应字节，不包含后续 Job Hash 中的 Payload。

停止 Run 的 Context 后停止 intake，交还未执行消息，保持在途任务续租。超过 ShutdownTimeout 后取消 Handler。Handler 超时或失去租约后，实际 goroutine 未退出仍占执行槽；Worker 强制停机后可能残留忽略 Context 的业务 goroutine。需要强杀能力时应由应用使用进程隔离。

Observer 异步、有界、有损，必须响应 Context。EventFinished.Err 是执行结果，CommitErr 是状态提交结果。指标包 `core/scheduler/metrics` 提供 Prometheus Observer；队列快照由应用调用 Client.Stats 后通过 SetQueueStats/SetObserverDropped 发布，采集器不主动访问 Redis。

普通 Redis 操作继承取消并设置 OperationTimeout，停机结果提交使用独立有界 Context。借用的 go-redis 客户端必须启用 ContextTimeoutEnabled=true；构造 Client/Worker 时会检查配置，未启用则返回 ErrInvalidArgument，不修改调用方的连接选项。请同时合理配置 Dial/Read/Write/PoolTimeout。Client.Close 关闭提交和 Observer；管理句柄继续借用 Redis 访问实体。Worker 不关闭 Redis 连接。

## 可选执行历史

```go
worker, err := scheduler.NewWorker(redisClient, scheduler.WorkerConfig{
 Namespace: "app",
 History: scheduler.HistoryConfig{MaxEntries: 20, TTL: 72*time.Hour},
})
attempts, err := client.Jobs().Attempts(ctx, jobID)
```

History 默认关闭。启用后每个 Job 最多保留指定条数（上限 1000），TTL 自最后一次写入刷新。历史在完成、失败和失效执行恢复时由同一 Lua 原子写入，结果回执重放不会重复追加。列表最新在前，Job 删除后仍保留到历史 TTL 到期；空列表表示未启用、无完成记录或历史已过期，不用于判断 Job 是否存在。

内部实现与 Key、Lua、编解码全部位于 `internal/store`。`internal/model` 保存共享领域值，Client 和 Worker 不共享执行引擎。目录及边界验收见 ARCHITECTURE.md。

## Redis 协议

Key 前缀为 `scheduler:{namespace}:v1:`，同 namespace 共享 slot；通过 PartitionNamespace 跨 slot 扩展，按 ID 管理需保存 namespace。Lua 实现状态转换，XREADGROUP CLAIM 参与重投递，XAUTOCLAIM 提供受预算公平扫描，XACKDEL DELREF 清理单执行组消息，XNACK 交还未执行消息，XADD IDMP 按 Job/generation/attempt 防止重复投递。

Client 第一次提交、创建 Schedule，以及 Worker.Run，预检 Redis 8.10 和必需命令。所有 Cluster 节点应运行相同版本及 ACL 配置。Lua 原子执行不代表运行时错误回滚；不要由其他程序修改内部 key 类型、删除消费组或修改 Stream 幂等配置。

执行保证为 at-least-once。幂等回执仅保护调度状态，外部副作用仍需业务幂等。默认 Succeeded 立即删除，Cancelled/Expired 保留 1 小时，Dead 72 小时，终态 Schedule 7 天，提交回执 24 小时、完成回执 1 小时。生产部署使用适合任务数据的持久化、复制和容量配置；内存淘汰不能替代保留策略。

## 验证

```bash
SCHEDULER_INTEGRATION_REQUIRED=1 go test -race ./core/scheduler/... -count=3
go vet ./core/scheduler/...
go test ./...
go test ./core/scheduler -run '^$' -bench 'Benchmark(TaskRoundTrip|ConcurrentRoundTrip)$' -benchmem -benchtime=200ms
```

REDIS_ADDR 默认 localhost:6379，REDIS_PASSWORD 默认本地测试密码；可通过环境变量覆盖。测试只清理独立随机 namespace，不执行 FLUSHDB/FLUSHALL。不设置 SCHEDULER_INTEGRATION_REQUIRED 时 Redis 不可用会跳过集成测试。

## 回执容量与隔离故障验证

Retention.Submission 默认 24h，约束提交重发窗口；Retention.Completion 默认 1h，约束执行结果重放窗口，可分别调整。新建 Job 使用独立 incarnation 作为 Stream 投递身份，JobID 在提交窗口之后复用不会撞到旧的 IDMP 记录。Claim 对相同 token/worker 的有效租约重放返回原快照，不延长租约、不增加执行计数。

回执 Key 数量大致为 `每秒提交数 × 提交保留秒数 + 每秒完成的执行数 × 完成保留秒数`。本地 Redis 8.10 MEMORY USAGE 样本：提交回执约 206B、完成回执约 212B；不包含全局 Key 管理、分配器碎片、Stream/IDMP、Job 或历史开销。按实际吞吐和可接受重放窗口配置 TTL。

```bash
go test ./core/scheduler -run '^$' -bench '^BenchmarkReceiptFootprint$' -benchtime=200ms
```

当前仓库没有自动创建 Cluster/AOF 环境的脚本。隔离环境测试位于 `redis_environment_test.go`，需要测试专用的 Redis 部署及显式环境变量；普通测试不会自动执行故障切换和持久化重启。仅运行普通集成测试不能证明这些部署场景已经验证。
