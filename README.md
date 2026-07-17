# Kit

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/kochabx/kit)](https://goreportcard.com/report/github.com/kochabx/kit)

Kit 是一个面向企业级场景的 Go 微服务工具包，覆盖应用生命周期、配置管理、依赖注入、认证安全、网络传输、数据存储、分布式任务、日志与可观测性等常见基础能力。

它的定位不是强绑定的大框架，而是一组可独立使用、自由组合的基础模块。每个模块边界清晰，可按需引入，帮助你在构建现代 Go 服务时减少重复的基础设施代码。

## 快速开始

### 安装

```bash
go get github.com/kochabx/kit
```

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

	application := app.New(
		app.WithServer(kithttp.NewServer(mux, kithttp.WithAddr(":8080"))),
	)

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

	application := app.New(
		app.WithServer(kithttp.NewServer(r, kithttp.WithAddr(":8080"))),
	)

	if err := application.Run(); err != nil {
		panic(err)
	}
}
```

### 生产化示例：配置 + 日志 + 数据库 + Redis + HTTP 服务

```go
package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kochabx/kit/app"
	"github.com/kochabx/kit/config"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/store/db"
	"github.com/kochabx/kit/store/redis"
	kithttp "github.com/kochabx/kit/transport/http"
)

type AppConfig struct {
	DB struct {
		Host     string `yaml:"host"     default:"localhost"`
		Port     int    `yaml:"port"     default:"3306"`
		User     string `yaml:"user"     default:"root"`
		Password string `yaml:"password"`
		Database string `yaml:"database" default:"mydb"`
	} `yaml:"db"`
	Redis struct {
		Host     string `yaml:"host"     default:"localhost"`
		Port     int    `yaml:"port"     default:"6379"`
		Password string `yaml:"password"`
	} `yaml:"redis"`
}

func main() {
	cfg := &AppConfig{}
	if err := config.New(cfg).Load(); err != nil {
		panic(err)
	}

	logger := log.New()
	log.SetGlobal(logger)

	gormDB, err := db.New(&db.MySQLConfig{
		Host:     cfg.DB.Host,
		Port:     cfg.DB.Port,
		User:     cfg.DB.User,
		Password: cfg.DB.Password,
		Database: cfg.DB.Database,
	})
	if err != nil {
		panic(err)
	}

	rdb, err := redis.New(&redis.Config{
		Addrs:    []string{cfg.Redis.Host + ":" + strconv.Itoa(cfg.Redis.Port)},
		Password: cfg.Redis.Password,
	})
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	// db.Client 和 redis.Client 均实现 cx.Starter/Stopper/HealthChecker，
	// 由 app 统一管理启动（Ping 验证连通性）和关闭。
	application := app.New(
		app.WithServer(kithttp.NewServer(mux, kithttp.WithAddr(":8080"))),
		app.WithComponent("db", gormDB),
		app.WithComponent("redis", rdb),
		app.WithShutdownTimeout(30*time.Second),
	)

	if err := application.Run(); err != nil {
		log.Fatal().Err(err).Msg("application exited with error")
	}
}
```

## 开发命令

```bash
make all          # 完整本地校验（fmt + vet + test）
make fmt          # 代码格式化
make vet          # 静态检查
make test         # 全量测试（含 race detector）
make build        # 编译验证

make install      # 安装工具依赖
make upgrade      # 升级依赖
make mod-tidy     # 整理并校验 Go 模块依赖

make generate     # 生成所有代码（proto + wire + swag）
make proto        # 生成 protobuf 代码
make wire         # 运行 wire 依赖注入代码生成
make swag         # 生成 Swagger 文档

make clean        # 清理生成物
make info         # 显示项目信息
make help         # 查看所有命令
```

## 上手路径

如果你是第一次接入，建议按以下顺序了解：

1. **[app](app/) + [config](config/README.md)**：理解应用启动与配置模型
2. **[log](log/README.md) + [errors](errors/)**：建立日志与错误规范
3. **[transport/http](transport/http/) 或 [transport/grpc](transport/grpc/)**：搭建服务入口
4. 按需接入 **[store/db](store/db/)、[store/redis](store/redis/README.md)、[cx](cx/README.md)**
5. 高阶能力：**[core/scheduler](core/scheduler/README.md)、[core/rate](core/rate/)、[core/auth/jwt](core/auth/jwt/)**

## 许可证

本项目基于 [MIT License](LICENSE) 发布。
