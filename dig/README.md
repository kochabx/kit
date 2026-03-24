# dig

轻量级依赖注入容器，参考 [Uber dig/fx](https://github.com/uber-go/dig) 设计风格，专注于**组件生命周期管理**与**依赖注入**。

## 特性

- 组件按 **Group**（组）分类管理，组间/组内均支持自定义初始化顺序
- 内置五个默认组：`config → database → service → handler → controller`（按 order 升序启动）
- 支持 `Provider / Consumer` 接口实现无反射依赖注入，并自动检测循环依赖
- 泛型 `Invoke[T]` / `MustInvoke[T]` 提供类型安全检索
- `MustProvide*` 系列方法可直接在 `init()` 中调用，无需处理 error
- 完整生命周期钩子：`OnStart / OnStarted / OnStopping / OnStop`
- 全局实例 `dig.C`，开箱即用

## 快速上手

```go
import "github.com/kochabx/kit/dig"

type RedisClient struct{ /* ... */ }
func (r *RedisClient) Name() string                  { return "redis" }
func (r *RedisClient) Start(ctx context.Context) error { /* connect */ return nil }
func (r *RedisClient) Stop(ctx context.Context) error  { /* close   */ return nil }

func init() {
    dig.C.MustProvideDatabase(&RedisClient{})
}

func main() {
    if err := dig.C.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer dig.C.Stop(context.Background())
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
| `Provider` | `ProvidesDependency() / GetDependency()` | 提供依赖 |
| `Consumer` | `RequiredDependencies() / SetDependency()` | 声明并接收依赖 |

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
client, err := dig.Invoke[*RedisClient](c, dig.DatabaseGroup, "redis")
client      := dig.MustInvoke[*RedisClient](c, dig.DatabaseGroup, "redis")
```

## 生命周期

```go
c := dig.New(
    dig.WithOnStart(func(ctx context.Context) error { /* before first Start */ return nil }),
    dig.WithOnStarted(func(ctx context.Context) error { /* after all started */ return nil }),
    dig.WithOnStopping(func(ctx context.Context) error { /* before first Stop */ return nil }),
    dig.WithOnStop(func(ctx context.Context) error { /* after all stopped  */ return nil }),
    dig.WithStopTimeout(10 * time.Second),
)

c.Start(ctx)
c.Stop(ctx)
c.Restart(ctx)
c.State() // StateNew / StateStarting / StateRunning / StateStopping / StateStopped / StateError
```

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
    dig.C.MustProvideService(&TokenProvider{})
    dig.C.MustProvideService(&APIClient{})
}
// 调用 dig.C.Start(ctx) 时自动完成注入
```

## 健康检查

```go
report := c.HealthCheck(ctx)
fmt.Println(report.Healthy) // true / false
for _, ch := range report.Components {
    fmt.Printf("%s: healthy=%v err=%v\n", ch.Name, ch.Healthy, ch.Error)
}
```
