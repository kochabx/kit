# cx

轻量级依赖注入容器，参考 [Uber dig/fx](https://github.com/uber-go/dig) 设计风格，专注于**组件生命周期管理**与**依赖注入**。

## 特性

- 组件按 **Group**（组）分类管理，组间/组内均支持自定义初始化顺序
- 内置五个默认组：`config → database → service → handler → controller`（按 order 升序启动）
- 支持 `Provider / Consumer` 接口实现无反射依赖注入，并自动检测循环依赖与重复 Provider
- 泛型 `Invoke[T]` / `MustInvoke[T]` 提供类型安全检索
- `MustProvide*` 系列方法可直接在 `init()` 中调用，无需处理 error
- 完整生命周期钩子：`OnStart / OnStarted / OnStopping / OnStop`
- Start 失败自动回滚已启动组件；Stop 聚合所有错误而非静默丢弃
- Provide 状态保护：仅 `StateNew` / `StateStopped` 时允许注册
- 依赖图导出：`DependencyGraph()` 返回组件间依赖关系
- 全局实例 `cx.C`，开箱即用

## 快速上手

```go
import "github.com/kochabx/kit/cx"

type RedisClient struct{ /* ... */ }
func (r *RedisClient) Name() string                    { return "redis" }
func (r *RedisClient) Start(ctx context.Context) error { /* connect */ return nil }
func (r *RedisClient) Stop(ctx context.Context) error  { /* close   */ return nil }

func init() {
    cx.C.MustProvideDatabase(&RedisClient{})
}

func main() {
    if err := cx.C.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer cx.C.Stop(context.Background())
    // ...
}
```

## 接口一览

| 接口 | 方法 | 说明 |
|------|------|------|
| `Component` | `Name() string` | 所有组件必须实现 |
| `Starter` | `Start(ctx) error` | 启动时调用 |
| `Stopper` | `Stop(ctx) error` | 停止时调用（逆序） |
| `Checker` | `Check(ctx) error` | 健康检查 |
| `Orderable` | `Order() int` | 自定义组内启动顺序 |
| `Provider` | `ProvidesDependency() / GetDependency()` | 提供依赖（内嵌 Component） |
| `Consumer` | `RequiredDependencies() / SetDependency()` | 声明并接收依赖（内嵌 Component） |

## 默认 Group

| 常量 | 值 | Order |
|------|----|-------|
| `ConfigGroup` | `"config"` | -100 |
| `DatabaseGroup` | `"database"` | -80 |
| `ServiceGroup` | `"service"` | -60 |
| `HandlerGroup` | `"handler"` | -40 |
| `ControllerGroup` | `"controller"` | -20 |

## 注册 API

```go
// 通用
c.Provide(groupName, comp, order...)
c.MustProvide(groupName, comp, order...)

// 便捷方法
c.MustProvideConfig(comp)
c.MustProvideDatabase(comp)
c.MustProvideService(comp)
c.MustProvideHandler(comp)
c.MustProvideController(comp)

// 新建自定义 Group
c.ProvideGroup("custom", 10)
```

## 检索 API

```go
comp, err := c.Get(groupName, name)
comp     := c.MustGet(groupName, name)
comps, _ := c.GetAll(groupName)

// 类型安全（泛型）
client, err := cx.Invoke[*RedisClient](c, cx.DatabaseGroup, "redis")
client      := cx.MustInvoke[*RedisClient](c, cx.DatabaseGroup, "redis")
```

## 查询 API

```go
c.Groups()              // 返回所有 group 名称（已排序）
c.HasGroup("service")   // group 是否存在
c.Has("service", "svc") // 组件是否存在
c.Count()               // 所有组件总数
c.State()               // 当前生命周期状态
```

## 生命周期

```go
c := cx.New(
    cx.WithOnStart(func(ctx context.Context) error { /* before first Start */ return nil }),
    cx.WithOnStarted(func(ctx context.Context) error { /* after all started */ return nil }),
    cx.WithOnStopping(func(ctx context.Context) error { /* before first Stop */ return nil }),
    cx.WithOnStop(func(ctx context.Context) error { /* after all stopped  */ return nil }),
    cx.WithStopTimeout(10 * time.Second),
)

c.Start(ctx)   // 依赖解析 → onStart → 按组启动 → onStarted
c.Stop(ctx)    // onStopping → 逆序停止 → onStop（错误聚合返回）
c.Restart(ctx) // Stop + Start
c.State()      // StateNew / StateStarting / StateRunning / StateStopping / StateStopped / StateError
```

**Start 失败回滚**：若某组件 Start 失败，已启动的组件会按逆序自动 Stop。

**Stop 错误聚合**：所有组件和钩子的 Stop 错误通过 `errors.Join` 聚合返回，不会因单个失败而跳过后续组件。

## 依赖注入

```go
// Provider — 暴露一个依赖值
type TokenProvider struct{}
func (t *TokenProvider) Name() string              { return "token-provider" }
func (t *TokenProvider) ProvidesDependency() string { return "auth-token" }
func (t *TokenProvider) GetDependency() any         { return "secret" }

// Consumer — 声明并接收依赖
type APIClient struct{ token string }
func (a *APIClient) Name() string                       { return "api-client" }
func (a *APIClient) RequiredDependencies() []string      { return []string{"auth-token"} }
func (a *APIClient) SetDependency(name string, v any) error {
    if name == "auth-token" { a.token = v.(string) }
    return nil
}

func init() {
    cx.C.MustProvideService(&TokenProvider{})
    cx.C.MustProvideService(&APIClient{})
}
// 调用 cx.C.Start(ctx) 时自动完成注入
```

自动检测：**循环依赖**（`ErrCircularDependency`）、**重复 Provider**（`ErrDuplicateProvider`）、**缺失依赖**（`ErrDependencyNotFound`）。

## 依赖图导出

```go
graph := c.DependencyGraph() // map[string][]string
// key: 组件名, value: 它所依赖的组件名列表（已排序）
// 例: {"api-client": ["token-provider"], "token-provider": []}
```

`Start()` 成功后可用，未启动时返回 `nil`。

## 健康检查与指标

```go
report := c.HealthCheck(ctx)
fmt.Println(report.Healthy) // true / false
for _, ch := range report.Components {
    fmt.Printf("%s: healthy=%v err=%v\n", ch.Name, ch.Healthy, ch.Error)
}

m := c.Metrics() // ContainerMetrics{GroupCount, ComponentCount, State}
```

## 哨兵错误

| 错误 | 说明 |
|------|------|
| `ErrGroupNotFound` | 指定的 group 不存在 |
| `ErrComponentNotFound` | 指定的组件不存在 |
| `ErrComponentAlreadyExists` | 同名组件已注册 |
| `ErrGroupAlreadyExists` | 同名 group 已存在 |
| `ErrInvalidComponent` | 组件为 nil 或 Name 为空 |
| `ErrDependencyNotFound` | Consumer 依赖的 key 无对应 Provider |
| `ErrCircularDependency` | 检测到依赖循环 |
| `ErrDuplicateProvider` | 多个 Provider 声明同一 key |
| `ErrNotRegisterable` | 非 New/Stopped 状态下调用 Provide |
| `ErrNotRunning` | 非运行状态下执行运行时操作 |
| `ErrAlreadyStarted` | 容器已启动 |
