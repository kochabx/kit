# cx

`cx` 是显式、类型化的依赖容器和严格生命周期管理器。

## 类型化注册

```go
var (
    configKey = cx.NewKey[*Config]("config")
    dbKey     = cx.NewKey[*DB]("db")
)

c := cx.New()
cx.MustSupply(c, configKey, cfg)
cx.MustProvide(c, dbKey, func(c *cx.Container) (*DB, error) {
    return NewDB(cx.MustGet(c, configKey))
})
```

同一个 `Key[T]` 同时约束注册值和读取值的类型。名称只用于唯一性检查、诊断和依赖图。

不作为构造参数、但需要约束生命周期顺序的依赖可显式声明：

```go
cx.MustProvide(c, workerKey, newWorker,
    cx.DependsOn(dbKey, redisKey),
)
```

构造函数中的 `Get` 依赖与 `DependsOn` 会合并为同一张依赖图，共享循环检测和拓扑顺序。

## 生命周期

组件可按需实现：

```go
type Starter interface { Start(context.Context) error }
type Stopper interface { Stop(context.Context) error }
type HealthChecker interface { HealthCheck(context.Context) error }
```

构造和启动按依赖顺序执行，停止按逆序执行。Container 从构造成功开始拥有 `Stopper`；因此构造后任何启动阶段失败，包含失败组件本身在内的所有已构造资源都会回滚。

停止错误不会丢失实例：Container 进入 `StateStopFailed`，保留失败资源并允许再次 `Stop`。组件的 `Stop` 应当幂等并支持部分初始化后的清理。

关闭期限可以分别配置：

```go
c := cx.New(
    cx.WithShutdownTimeout(30*time.Second),
    cx.WithComponentStopTimeout(10*time.Second),
    cx.WithRollbackTimeout(20*time.Second),
)
```

成功执行的停止 Hook 会被记录；`Stop` 重试只重新执行失败的 Hook 和组件。

```text
New/Stopped/Failed -> Starting -> Running -> Stopping -> Stopped
                         |                       |
                         +-> Failed             +-> StopFailed
                         +-> StopFailed <-------------+
```

## 后台任务监督

`Runner` 将阻塞式 `Run(context.Context) error` 适配为生命周期组件：

```go
runner := cx.MustRunner(scheduler)
runnerKey := cx.NewKey[*cx.Runner]("scheduler")
cx.MustSupply(c, runnerKey, runner)
```

普通函数使用 `RunnerFunc`：

```go
runner := cx.MustRunner(cx.RunnerFunc(runWorker))
```

`Start` 创建独立执行，`Stop` 请求取消并等待退出。停止超时后可以重试，不会启动重叠执行。Runner panic 会转换为 `ErrRunnerPanic`。

`Container.Wait(ctx)` 通过单一 Supervisor 监督所有 `Supervised` 组件。多个调用方共享同一退出结果，不会重复创建监督树。任意任务意外退出都会立即返回对应错误；`app.Application` 使用它触发全局优雅关闭。运行错误由监督路径报告，`Stop` 只报告停止失败。

需要显式处理构造错误时使用 `NewRunner`；wiring 阶段可使用 `MustRunner`。两者都会拒绝普通 nil 和 typed nil。

## 健康检查

```go
report, err := c.HealthCheck(ctx)
```

健康检查只允许在 `StateRunning` 执行，并区分：

- `HealthHealthy`
- `HealthUnhealthy`
- `HealthSkipped`：组件未实现 `HealthChecker`
- `HealthTimeout`

每个组件最多存在一个在途检查。即使组件错误地忽略 context，聚合检查仍会按时返回，后续请求复用超时结果，不会持续创建泄漏 goroutine。

## API

| API | 说明 |
|---|---|
| `NewKey[T](name)` | 创建类型化组件 Key |
| `Provide(c, key, ctor, options...)` | 注册惰性构造函数 |
| `Supply(c, key, value)` | 注册预构造值 |
| `DependsOn(keys...)` | 声明显式生命周期依赖 |
| `Get(c, key)` | 类型安全读取 |
| `MustProvide/MustSupply/MustGet` | wiring 阶段失败时 panic 的变体 |
| `NewRunner/MustRunner` | 创建受监督的后台任务适配器 |
| `Start/Stop/Restart` | 严格生命周期管理 |
| `Wait` | 监督后台任务退出 |
| `HealthCheck` | 状态化并发健康检查 |
| `DependencyGraph` | 返回最近一次构建的依赖图 |
