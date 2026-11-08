# app

应用生命周期管理：配置检查、组件启动、信号监听、运行监督、优雅关闭和健康检查。

## 用法

```go
server := kithttp.NewServer(handler, kithttp.WithAddr(":8080"))
application, err := app.New(
    app.WithServer(server),
    app.WithComponent(cx.NewKey[*db.Client]("db"), dbClient),
    app.WithShutdownTimeout(30*time.Second),
)
if err != nil {
    return err
}
return application.Run()
```

`app.New` 返回 `(*Application, error)`。它验证和注册配置，不启动组件；nil 选项、nil/typed nil Server、空组件键、nil 组件、nil 回调和非正超时都会返回错误。

## 生命周期

```text
New → Run（只运行一次）
        ├─ 注册信号监听
        ├─ 构建容器 → OnStart
        ├─ 启动基础组件 → 启动 Server → OnStarted
        ├─ 等待取消、信号或受监督组件退出
        └─ OnStopping → 停止 Server → 释放基础组件 → OnStop
```

基础组件在 Server 之前注册，与 `WithServer` 和 `WithComponent` 的选项排列无关。同类组件按注册顺序处理，显式声明的 cx 依赖关系优先。通过 `Container()` 后续注册的依赖不会自动排到 Server 前；优先使用 `WithComponent`，复杂依赖应通过 cx 声明。

运行中重复调用 `Run` 返回 `ErrAlreadyRunning`；运行结束或启动失败后重复调用返回 `ErrAlreadyRun`。需要重新运行时创建新 Application 及适合新生命周期的资源。

信号监听在启动前建立，因此阻塞的启动逻辑也能收到 Context 取消。启动钩子与组件必须配合检查 Context；启动失败的回滚由 cx 完成，取消导致的启动失败保留 `context.Canceled` 错误。

正常运行期间收到取消或信号会进入关闭流程。受监督组件异常退出会保留运行错误；若关闭也失败，通过 `errors.Join` 同时返回。关闭 Context 保留应用根 Context 的值，但拥有独立取消状态和超时。

## 选项

| 选项 | 说明 | 默认值 |
|---|---|---|
| `WithContext(ctx)` | 应用根 Context，必须非 nil | Background |
| `WithShutdownTimeout(d)` | 应用关闭预算，必须为正 | 30s |
| `WithSignals(sigs...)` | 替换信号列表；空列表禁用监听 | SIGINT、SIGTERM、SIGQUIT |
| `WithServer(s)` / `WithServers(...)` | 注册最后启动、最先停止的 Server | 无 |
| `WithComponent(key, value)` | 注册基础组件 | 无 |
| `WithOnStart(fn)` | 所有组件启动前 | 无 |
| `WithOnStarted(fn)` | 所有组件启动后 | 无 |
| `WithOnStopping(fn)` | 关闭组件前 | 无 |
| `WithOnStop(fn)` | 关闭组件后 | 无 |
| `WithContainer(c)` | 接管外部新容器的生命周期 | 内部创建 |

## 外部容器

```go
container := cx.New(cx.WithOnStarted(onStarted))
if err := container.Supply(cx.NewKey[*db.Client]("db"), dbClient); err != nil {
    return err
}
// 在 container 中继续配置 Server，并声明需要的依赖关系。
application, err := app.New(app.WithContainer(container))
if err != nil {
    return err
}
return application.Run()
```

`WithContainer` 要求容器处于 `cx.StateNew`，不能与 app 的组件、Server 或生命周期钩子注册选项混用；这些配置应在容器中完成。Context、信号和应用关闭超时仍可通过 app 配置。容器自身的关闭预算也有效，实际关闭受两个预算共同约束。

容器交给 Application 后，由应用负责启停，不要同时在其他地方启动或关闭。

## 关闭和健康检查

```go
application.Shutdown() // 并发安全，只请求取消，不等待关闭完成
report, err := application.HealthCheck(ctx)
```

等待 `Run` 返回即可确认关闭流程已完成。`HealthCheck` 返回 cx 的聚合健康报告。选项快照不会因调用方随后修改信号或 Server 切片而变化；应用运行时不要再修改容器注册。

## 验证

```bash
go test -race ./app ./cx -count=1
```

测试通过就绪通知同步启动状态，HTTP 使用动态端口；信号场景在专用子进程中运行。测试环境需要允许本地监听和子进程信号。
