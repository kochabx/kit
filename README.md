# Kit

Go 服务基础工具包，提供应用生命周期、配置、依赖注入、日志、校验、认证、加密、网络客户端、存储接入和 Redis 任务调度能力。模块可按需组合使用。

## 安装与使用

```bash
go get github.com/kochabx/kit
```

以下完整示例可直接作为 `main.go` 编译。模块 README 中未包含 `package main` 的代码块为用法片段，`ctx`、配置和业务函数由应用提供。

### 最小示例：启动一个 HTTP 服务（标准库）

HTTP 服务封装只需传入任意 `http.Handler`（原生 `mux`、`gin`、`chi` 等均可）：

```go
package main

import (
	"net/http"

	"github.com/kochabx/kit/app"
	kithttp "github.com/kochabx/kit/transport/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":"pong"}`))
	})

	application, err := app.New(
		app.WithServer(kithttp.NewServer(mux, kithttp.WithAddr(":8080"))),
	)

	if err != nil {
		panic(err)
	}

	if err := application.Run(); err != nil {
		panic(err)
	}
}
```

### 最小示例：启动一个 HTTP 服务（Gin）

```go
package main

import (
	"github.com/gin-gonic/gin"

	"github.com/kochabx/kit/app"
	kithttp "github.com/kochabx/kit/transport/http"
)

func main() {
	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	application, err := app.New(
		app.WithServer(kithttp.NewServer(r, kithttp.WithAddr(":8080"))),
	)

	if err != nil {
		panic(err)
	}

	if err := application.Run(); err != nil {
		panic(err)
	}
}
```

## HTTP 指标

```go
server := kithttp.NewServer(handler,
    kithttp.WithMetrics(kithttp.MetricsOption{}),
)
```

默认在 `/metrics` 导出独立 Registry 中的 Go Runtime 和 BuildInfo 指标。传入 `MetricsOption.Registry` 时使用该实例，不复制，也不自动向它注入默认 Collector；自定义指标由应用注册。Scheduler 的 Prometheus Observer 可以注册到同一个 Registry。

## 开发命令

以下目标以 [Makefile](Makefile) 为准：

```bash
make help          # 列出目标
make info          # 模块与工具链信息
make fmt           # 格式化 Go 源码
make fmt-check     # 检查格式
make vet           # 静态检查
make test          # 竞态测试，默认超时 120s
make check         # fmt-check + vet + test
make build         # 编译所有 Go 包
make deps          # 下载依赖
make tidy          # 整理 go.mod / go.sum
make deps-upgrade  # 升级依赖并整理模块
```

可通过 `PKGS`、`TEST_TIMEOUT` 和 `GO` 限定范围或工具链，例如：

```bash
make test PKGS=./core/validator TEST_TIMEOUT=60s
go test ./... -run '^$' # 只编译包和测试，不执行测试
```

全量测试包含本地监听端口、进程信号及外部服务场景。集成环境要求和跳过条件以各模块测试及 README 为准；不将“编译通过”等同于完整生产环境验证。
