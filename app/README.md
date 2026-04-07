# app

应用生命周期管理器，负责组件启动、信号监听、优雅关闭与健康检查。

## 快速开始

```go
package main

import (
    "github.com/kochabx/kit/app"
    "github.com/kochabx/kit/store/db"
    "github.com/kochabx/kit/transport/http"

    "github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
    r.GET("/ping", func(c *gin.Context) { c.String(200, "pong") })

    dbClient, _ := db.New(&db.PostgresConfig{DSN: "postgres://..."})

    a := app.New(
        app.WithServer(http.NewServer(r, http.WithAddr(":8080"))),
        app.WithComponent("db", dbClient),
    )
    if err := a.Run(); err != nil {
        panic(err)
    }
}
```

`Run()` 阻塞直到收到 `SIGINT` / `SIGTERM` / `SIGQUIT`，然后按注册的逆序优雅关闭所有组件。

## 生命周期

```
New() → Run()
              ├─ container.Start()     // 按注册顺序启动组件
              ├─ 阻塞等待信号 / ctx 取消
              └─ container.Stop()      // 按注册逆序关闭组件
```

组件只需实现 `cx.Starter` / `cx.Stopper` / `cx.Checker` 中的任意接口即可参与生命周期管理。`transport.Server` 已内置实现。

## Option

| Option | 说明 | 默认值 |
|---|---|---|
| `WithContext(ctx)` | 根上下文 | `context.Background()` |
| `WithShutdownTimeout(d)` | 优雅关闭超时 | 30s |
| `WithSignals(sigs...)` | 触发关闭的信号 | SIGINT, SIGTERM, SIGQUIT |
| `WithServer(s)` | 注册 transport.Server | - |
| `WithServers(s...)` | 批量注册 Server | - |
| `WithComponent(key, v)` | 注册自定义组件 | - |
| `WithContainer(c)` | 使用外部 cx.Container | 内部创建 |
| `WithOnStart(fn)` | 组件启动前钩子 | - |
| `WithOnStarted(fn)` | 组件启动后钩子 | - |
| `WithOnStopping(fn)` | 组件停止前钩子 | - |
| `WithOnStop(fn)` | 组件停止后钩子 | - |

## 健康检查

```go
report := a.HealthCheck(ctx)
fmt.Println(report.Healthy) // true / false
```

返回所有实现 `cx.Checker` 接口的组件的聚合健康状态。

## 手动关闭

```go
a.Shutdown() // 取消根上下文，触发 Run() 退出
```
