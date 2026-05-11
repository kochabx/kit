# cx

轻量级泛型依赖注入容器，专注于**类型安全**、**惰性构造**与**生命周期管理**。

## 特性

- **泛型 API** — `Provide[T]` / `Supply[T]` / `Get[T]`（含 `Must*` 变体）编译期类型安全
- **惰性构造** — 构造函数在 `Start()` 时按依赖顺序自动调用，无需手动排序
- **自动依赖排序** — 构造函数中调用 `Get` 获取依赖，容器自动追踪依赖关系并决定 Start/Stop 顺序
- **循环依赖检测** — 构造阶段自动检测，报错包含完整路径（如 `A → B → C → A`）
- **生命周期钩子** — `OnStart / OnStarted / OnStopping / OnStop` 四个时机
- **启动回滚** — 组件 N 启动失败，已启动的 1..N-1 自动逆序停止
- **停止错误聚合** — Stop 收集所有错误而非静默丢弃
- **可选接口** — 值实现 `Starter` / `Stopper` / `Checker` 即可参与生命周期，零强制接口
- **并发健康检查** — `HealthCheck` 并发执行所有 `Checker`，每个组件独立超时
- **依赖图导出** — `DependencyGraph()` 返回构造期记录的依赖边，便于调试与可视化
- **全局实例** — `cx.C` 开箱即用，`init()` 自注册模式无缝衔接
- **无 reflect 依赖** — 仅使用 Go 泛型与类型断言

## 快速上手

```go
package main

import (
    "context"
    "fmt"

    "github.com/kochabx/kit/cx"
)

type Config struct {
    DSN string
}

type DB struct {
    cfg *Config
}

func (d *DB) Start(ctx context.Context) error {
    fmt.Println("DB connected:", d.cfg.DSN)
    return nil
}

func (d *DB) Stop(ctx context.Context) error {
    fmt.Println("DB disconnected")
    return nil
}

func init() {
    // 注册配置（预构造值）—— 启动期注册错误是程序员错误，用 Must* 直接 panic
    cx.MustSupply(cx.C, "config", &Config{DSN: "postgres://localhost/mydb"})

    // 注册数据库（惰性构造函数）
    cx.MustProvide(cx.C, "db", func(c *cx.Container) (*DB, error) {
        cfg, err := cx.Get[*Config](c, "config")
        if err != nil {
            return nil, err
        }
        return &DB{cfg: cfg}, nil
    })
}

func main() {
    ctx := context.Background()

    // 启动：自动按依赖顺序构造 config → db，然后调用 db.Start()
    if err := cx.C.Start(ctx); err != nil {
        panic(err)
    }

    // 类型安全检索
    db, err := cx.Get[*DB](cx.C, "db")
    if err != nil {
        panic(err)
    }
    fmt.Printf("DB: %+v\n", db)

    // 停止：逆序调用 db.Stop()
    if err := cx.C.Stop(ctx); err != nil {
        panic(err)
    }
}
```

## 核心概念

### 注册

```go
// 构造函数注册（惰性，Start 时调用）
cx.Provide[T](c, "key", func(c *cx.Container) (T, error) { ... })

// 预构造值注册
cx.Supply[T](c, "key", value)

// Must 变体：注册失败直接 panic，适用于 init/main 启动期
cx.MustProvide[T](c, "key", ctor)
cx.MustSupply[T](c, "key", value)
```

### 检索

```go
val, err := cx.Get[T](c, "key")  // 类型安全，返回 error
val := cx.MustGet[T](c, "key")   // 检索失败直接 panic
```

### 依赖声明

在构造函数中调用 `Get` 即为声明依赖，容器自动追踪：

```go
cx.Provide(c, "service", func(c *cx.Container) (*Service, error) {
    db, err := cx.Get[*DB](c, "db")
    if err != nil {
        return nil, err
    }
    cache, err := cx.Get[*Cache](c, "cache")
    if err != nil {
        return nil, err
    }
    return &Service{db: db, cache: cache}, nil
})
```

构造顺序自动为：`db, cache → service`。

### 生命周期接口

全部可选，按需实现：

```go
type Starter interface { Start(ctx context.Context) error }  // 初始化
type Stopper interface { Stop(ctx context.Context) error }   // 清理
type Checker interface { Check(ctx context.Context) error }  // 健康检查
```

### 容器生命周期

```
StateNew → StateStarting → StateRunning → StateStopping → StateStopped
                                                ↓
                                           StateFailed
```

- **Start**：构造所有组件（惰性递归） → `onStart` 钩子 → 调用 `Starter.Start()`（依赖序） → `onStarted` 钩子
- **Stop**：`onStopping` 钩子 → 调用 `Stopper.Stop()`（构造逆序） → `onStop` 钩子 → 重置构造状态
- **Restart**：Stop + Start（构造函数重新调用）

### 容器选项

```go
c := cx.New(
    cx.WithStopTimeout(10 * time.Second),    // 每组件停止超时（默认 30s）
    cx.WithHealthTimeout(5 * time.Second),   // 每组件健康检查超时（默认 10s）
    cx.WithOnStart(func(ctx context.Context) error { ... }),
    cx.WithOnStarted(func(ctx context.Context) error { ... }),
    cx.WithOnStopping(func(ctx context.Context) error { ... }),
    cx.WithOnStop(func(ctx context.Context) error { ... }),
)
```

## API 速查

| 函数 | 说明 |
|------|------|
| `Provide[T](c, key, ctor)` | 注册惰性构造函数 |
| `Supply[T](c, key, val)` | 注册预构造值 |
| `MustProvide[T](c, key, ctor)` | 同 `Provide`，失败 panic |
| `MustSupply[T](c, key, val)` | 同 `Supply`，失败 panic |
| `Get[T](c, key)` | 类型安全检索 |
| `MustGet[T](c, key)` | 同 `Get`，失败 panic |
| `c.Start(ctx)` | 构造 + 启动所有组件 |
| `c.Stop(ctx)` | 逆序停止所有组件 |
| `c.Restart(ctx)` | Stop + Start |
| `c.HealthCheck(ctx)` | 聚合健康检查（并发） |
| `c.Metrics()` | 容器统计 |
| `c.DependencyGraph()` | 依赖边映射 `key → deps`（Start 后填充） |
| `c.Keys()` | 所有注册 key（注册序） |
| `c.Has(key)` | key 是否已注册 |
| `c.Count()` | 组件总数 |
| `c.State()` | 当前状态 |

## 错误类型

| 错误 | 场景 |
|------|------|
| `ErrComponentNotFound` | Get 时 key 未注册，或已注册但未构造（Start 之外调用） |
| `ErrComponentExists` | Provide 时 key 重复 |
| `ErrCircularDependency` | 构造阶段检测到循环依赖 |
| `ErrTypeMismatch` | Get[T] 类型断言失败 |
| `ErrContainerNotIdle` | 非 New/Stopped 状态下注册 |
| `ErrInvalidKey` | key 为空字符串 |

## 与 cxgen 配合

`cxgen` 工具自动扫描项目中所有导入 `cx` 且包含 `init()` 函数的包，生成空导入文件确保 `init()` 执行：

```bash
go run ./cmd/cxgen gen ./... -o wire_gen.go
```
