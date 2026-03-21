# Kit

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/kochabx/kit)](https://goreportcard.com/report/github.com/kochabx/kit)

Kit 是一个面向企业级场景的 Go 微服务工具包，覆盖应用生命周期、配置管理、依赖注入、认证安全、网络传输、数据存储、分布式任务、日志与可观测性等常见基础能力。

它的定位不是强绑定的大框架，而是一组可独立使用、自由组合的基础模块。每个模块边界清晰，可按需引入，帮助你在构建现代 Go 服务时减少重复的基础设施代码。

## 模块总览

### 应用与基础设施

| 模块 | 说明 |
|------|------|
| [app](app/) | 应用生命周期管理，多服务启动、信号处理、优雅关闭 |
| [config](config/) | 配置加载与热更新，支持 YAML/JSON/TOML、环境变量覆盖、结构体绑定与校验 |
| [ioc](ioc/) | 依赖注入容器，支持命名空间、模块化注册与生命周期管理 |
| [errors](errors/) | 结构化错误定义、包装与错误链处理 |
| [log](log/) | 基于 Zerolog 的结构化日志，支持脱敏、轮转与全局日志器 |

### 网络与通信

| 模块 | 说明 |
|------|------|
| [transport/http](transport/http/) | 框架无关的 HTTP 服务封装，任意 `http.Handler` 均可接入，内置 `/metrics`、`/swagger/` 端点 |
| [transport/http/middleware](transport/http/middleware/) | 认证、CORS、加解密、日志、权限、恢复、请求签名、XSS 防护中间件 |
| [transport/grpc](transport/grpc/) | gRPC 服务封装，与应用生命周期统一集成 |
| [transport/websocket](transport/websocket/) | WebSocket 客户端，支持自动重连、心跳保活、事件驱动与消息队列 |
| [core/httpx](core/httpx/) | HTTP 客户端工具，统一处理请求构建与响应读取 |

### 安全与认证

| 模块 | 说明 |
|------|------|
| [core/auth/jwt](core/auth/jwt/) | JWT 生成、校验、刷新，支持 JTI 管理与 Redis 集成 |
| [core/auth/mfa](core/auth/mfa/) | 基于 TOTP 的多因子认证 |
| [core/crypto/ecies](core/crypto/ecies/) | 基于椭圆曲线的非对称加密方案 |
| [core/crypto/hmac](core/crypto/hmac/) | HMAC 签名与完整性校验 |

### 存储与消息

| 模块 | 说明 |
|------|------|
| [store/db](store/db/) | GORM 封装，支持 MySQL、PostgreSQL、SQLite |
| [store/redis](store/redis/) | Redis 客户端，支持单机与集群模式 |
| [store/mongo](store/mongo/) | MongoDB 客户端封装 |
| [store/etcd](store/etcd/) | Etcd 客户端与辅助工具 |
| [store/kafka](store/kafka/) | Kafka 生产与消费封装 |
| [store/oss/minio](store/oss/minio/) | MinIO 对象存储客户端，支持分片上传与断点续传 |

### 分布式与通用能力

| 模块 | 说明 |
|------|------|
| [core/scheduler](core/scheduler/) | 基于 Redis 的分布式任务调度，支持延迟任务、Cron、优先级队列、去重、死信队列 |
| [core/rate](core/rate/) | 限流组件，支持令牌桶与滑动窗口（Lua 脚本实现） |
| [observability/metrics](observability/metrics/) | Prometheus 指标采集，集成至 HTTP 服务 |
| [core/validator](core/validator/) | 数据校验，支持多语言错误输出与自定义规则 |
| [core/defaults](core/defaults/) | 结构体标签默认值注入，支持嵌套、切片、Map 等场景 |
| [core/util](core/util/) | 上下文工具、类型转换、ID 生成、树结构、二维码等通用工具 |

## 快速开始

### 安装

```bash
go get github.com/kochabx/kit
```

### 最小示例：启动一个 HTTP 服务

HTTP 服务封装是框架无关的，只需传入任意 `http.Handler`（原生 `mux`、`gin`、`chi` 等均可）：

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
		app.WithServer(kithttp.NewServer(":8080", mux)),
	)

	if err := application.Start(); err != nil {
		panic(err)
	}
}
```

### 生产化示例：配置 + 日志 + 数据库 + Redis + HTTP 服务

```go
package main

import (
	"context"
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

	gormDB, err := db.NewGorm(&db.MySQLConfig{
		Host:     cfg.DB.Host,
		Port:     cfg.DB.Port,
		User:     cfg.DB.User,
		Password: cfg.DB.Password,
		Database: cfg.DB.Database,
	})
	if err != nil {
		panic(err)
	}

	rdb, err := redis.NewClient(&redis.SingleConfig{
		Host:     cfg.Redis.Host,
		Port:     cfg.Redis.Port,
		Password: cfg.Redis.Password,
	})
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	application := app.New(
		app.WithServer(kithttp.NewServer(":8080", mux)),
		app.WithShutdownTimeout(30*time.Second),
		app.WithClose("database", func(ctx context.Context) error {
			return gormDB.Close()
		}, 5*time.Second),
		app.WithClose("redis", func(ctx context.Context) error {
			return rdb.Close()
		}, 3*time.Second),
	)

	log.Info().Msg("starting application")
	if err := application.Start(); err != nil {
		log.Fatal().Err(err).Msg("failed to start application")
	}
}
```

## 模块说明

### [app](app/)

管理多个 `transport.Server` 的统一启动与关闭：

- 并发启动多个服务，任一服务退出时触发全局关闭
- 监听 `SIGINT`/`SIGTERM` 信号，支持自定义信号列表
- 注册带超时的资源关闭函数（数据库、缓存等），按注册顺序执行
- 支持通过 `WithContext` 传入根上下文

```go
app.New(
    app.WithServer(httpServer),
    app.WithServer(grpcServer),
    app.WithShutdownTimeout(30 * time.Second),
    app.WithClose("redis", rdb.Close, 3*time.Second),
)
```

### [config](config/README.md)

基于 [Viper](https://github.com/spf13/viper) 的配置管理：

- 默认读取 `./config.yaml`，支持自定义路径与文件名
- 支持环境变量覆盖（大写键名自动映射）
- 通过 `config.New(target)` 解绑到任意结构体，自动注入 `default` 标签默认值
- 支持 `Watch()` 热更新配置，触发用户回调

### [ioc](ioc/README.md)

轻量级依赖注入容器：

- `ContainerBuilder` 提供流式 API，支持内置命名空间（`config`、`database`、`baseComponent`、`handler`、`controller`）及自定义顺序
- 按命名空间优先级有序初始化与关闭，便于管理复杂服务依赖

```go
container, err := ioc.NewContainerBuilder().
    WithDefaultNamespaces().
    Build()
```

### [transport/http](transport/http/)

框架无关的 HTTP 服务，接受任意 `http.Handler`：

- 内置 `/metrics` Prometheus 端点（可配置路径）
- 内置 `/swagger/` Swagger UI 端点
- 支持 TLS 配置、超时参数及服务命名
- 实现 `transport.Server` 接口，直接接入 `app.Application`

### [transport/http/middleware](transport/http/middleware/README.md)

可按需组合的 HTTP 中间件：

| 中间件 | 功能 |
|--------|------|
| `Auth` | JWT 身份校验 |
| `CORS` | 跨域资源共享 |
| `Crypto` | 请求/响应加解密 |
| `Logger` | 结构化请求日志 |
| `Permission` | 权限校验 |
| `Recovery` | Panic 恢复 |
| `Signature` | 请求签名验证 |
| `XSS` | XSS 过滤与防护 |

### [log](log/README.md)

基于 [Zerolog](https://github.com/rs/zerolog) 的高性能结构化日志：

- 支持控制台输出与文件输出，可配置日志级别
- 内置日志轮转（基于 `lumberjack`）
- `desensitize` 子包提供字段级脱敏能力，支持手机号、邮箱、身份证等内置规则，也支持自定义规则
- 全局日志器 `log.SetGlobal(logger)` 方便全局调用

### [errors](errors/)

结构化错误类型 `Error`，携带 HTTP 状态码、业务消息与元数据：

- `errors.New(code, message)` 创建结构化错误
- `errors.Wrap(err, ...)` 逐层包装原始错误
- 与标准库 `errors.Is` / `errors.As` 完全兼容

### [core/scheduler](core/scheduler/README.md)

基于 Redis Streams 的分布式任务调度器：

- **延迟任务**：指定执行时间或延迟时长
- **Cron 任务**：类 Linux crontab 表达式
- **优先级队列**：不同优先级任务分别调度
- **去重**：防止同一任务重复入队
- **分布式锁**：确保任务单实例执行
- **重试策略**：指数退避、固定间隔等
- **死信队列**：失败任务自动转移，便于排查

### [core/rate](core/rate/)

Redis 支持的分布式限流：

- **令牌桶**：适合平滑流量的突发场景
- **滑动窗口**：精确统计时间窗口内的请求数
- 均通过 Lua 脚本原子执行，保证分布式一致性

### [store](store/)

统一的存储层封装，各组件均支持 functional options 配置：

- **db**：`db.NewGorm(config)` 支持 MySQL / PostgreSQL / SQLite
- **redis**：`redis.NewClient(config)` 支持单机 / Sentinel / Cluster 模式
- **mongo**：`mongo.NewClient(config)` MongoDB 客户端
- **etcd**：`etcd.NewClient(config)` + 辅助函数（Watch、选举等）
- **kafka**：`kafka.NewClient(config)` 生产者与消费者
- **oss/minio**：`minio.NewClient(config)` 支持分片上传与断点续传

## 项目结构

```text
kit/
├── app/                     # 应用生命周期管理
├── config/                  # 配置管理
├── errors/                  # 错误定义与包装
├── ioc/                     # 依赖注入容器
├── log/                     # 日志系统
│   ├── desensitize/         # 脱敏子包
│   └── writer/              # 输出目标（控制台、文件、轮转）
├── observability/
│   └── metrics/http/        # HTTP Prometheus 指标
├── store/
│   ├── db/                  # GORM 数据库
│   ├── etcd/                # Etcd
│   ├── kafka/               # Kafka
│   ├── mongo/               # MongoDB
│   ├── oss/minio/           # MinIO 对象存储
│   └── redis/               # Redis
├── transport/
│   ├── transport.go         # Server 接口定义
│   ├── grpc/                # gRPC 服务
│   ├── http/                # HTTP 服务与中间件
│   └── websocket/           # WebSocket 客户端
└── core/
    ├── auth/jwt/            # JWT 认证
    ├── auth/mfa/            # TOTP 多因子认证
    ├── crypto/ecies/        # ECIES 非对称加密
    ├── crypto/hmac/         # HMAC 签名
    ├── defaults/            # 结构体默认值注入
    ├── httpx/               # HTTP 客户端
    ├── rate/                # 分布式限流
    ├── scheduler/           # 分布式任务调度
    ├── util/                # 通用工具
    └── validator/           # 数据校验
```

## 开发命令

```bash
make fmt          # 代码格式化
make vet          # 静态检查
make test         # 全量测试（含 race detector）
make build        # 编译验证

make install      # 安装工具依赖
make upgrade      # 升级依赖

make proto        # 生成 protobuf 代码
make wire         # 运行 wire 依赖注入代码生成
make swag         # 生成 Swagger 文档

make help         # 查看所有命令
```

## 上手路径

如果你是第一次接入，建议按以下顺序了解：

1. **[app](app/) + [config](config/README.md)**：理解应用启动与配置模型
2. **[log](log/README.md) + [errors](errors/)**：建立日志与错误规范
3. **[transport/http](transport/http/) 或 [transport/grpc](transport/grpc/)**：搭建服务入口
4. 按需接入 **[store/db](store/db/)、[store/redis](store/redis/README.md)、[ioc](ioc/README.md)**
5. 高阶能力：**[core/scheduler](core/scheduler/README.md)、[core/rate](core/rate/)、[core/auth/jwt](core/auth/jwt/)**

## 许可证

本项目基于 [MIT License](LICENSE) 发布。