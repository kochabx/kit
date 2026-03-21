# Kit

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/kochabx/kit)](https://goreportcard.com/report/github.com/kochabx/kit)

Kit 是一个面向企业级场景的 Go 微服务工具包，覆盖应用生命周期、配置管理、依赖注入、认证安全、网络传输、数据存储、分布式任务、日志与指标等常见基础能力。

它的定位不是提供一套强绑定的大框架，而是提供一组可独立使用、也可自由组合的基础模块，帮助你在构建现代 Go 服务时减少重复基础设施代码，同时保持清晰的工程边界。

## 模块总览

### 应用与基础设施

- [app](app/)：应用生命周期管理，支持多服务启动、信号处理、优雅关闭
- [config](config/)：配置加载与热更新，支持环境变量覆盖、结构体绑定与校验
- [ioc](ioc/)：轻量级依赖注入容器，支持作用域、命名空间和生命周期管理

### 安全与认证

- [core/auth/jwt](core/auth/jwt/)：JWT 生成、校验、刷新、JTI 管理与 Redis 集成
- [core/auth/mfa](core/auth/mfa/)：基于 TOTP 的多因子认证
- [core/crypto/ecies](core/crypto/ecies/)：基于椭圆曲线的加密方案
- [core/crypto/hmac](core/crypto/hmac/)：HMAC 签名和完整性校验

### 存储与消息

- [store/db](store/db/)：GORM 封装，支持 MySQL、PostgreSQL、SQLite
- [store/redis](store/redis/)：Redis 客户端与连接池配置
- [store/mongo](store/mongo/)：MongoDB 客户端封装
- [store/etcd](store/etcd/)：Etcd 客户端与辅助能力
- [store/kafka](store/kafka/)：Kafka 生产与消费封装
- [store/oss/minio](store/oss/minio/)：MinIO 对象存储客户端

### 网络与通信

- [transport/http](transport/http/)：HTTP 服务封装
- [transport/http/middleware](transport/http/middleware/)：HTTP 中间件集合
- [transport/grpc](transport/grpc/)：gRPC 服务封装
- [transport/grpc/middleware](transport/grpc/middleware/)：gRPC 中间件
- [transport/websocket](transport/websocket/)：WebSocket 客户端
- [core/httpx](core/httpx/)：HTTP 客户端工具

### 分布式与通用能力

- [core/scheduler](core/scheduler/)：基于 Redis 的分布式任务调度
- [core/rate](core/rate/)：限流组件，支持令牌桶与滑动窗口
- [log](log/)：高性能结构化日志，支持脱敏、轮转、全局日志器
- [observability/metrics](observability/metrics/)：Prometheus 指标采集
- [core/validator](core/validator/)：基于 validator 的数据校验
- [errors](errors/)：结构化错误定义、包装与错误链处理
- [core/defaults](core/defaults/)：结构体默认值注入与标签解析
- [core/util](core/util/)：上下文、转换、ID、树结构、二维码等工具集

## 适用场景

Kit 适合以下类型的项目：

- 需要快速搭建 HTTP 或 gRPC 服务的业务系统
- 需要统一日志、配置、错误模型的中后台服务
- 需要 Redis、数据库、Kafka、Etcd 等常见基础设施接入
- 需要任务调度、限流、认证、脱敏等通用能力的微服务系统
- 希望保留工程自主性，而不是被大型框架强绑定的 Go 项目

## 快速开始

### 安装

```bash
go get github.com/kochabx/kit
```

### 最小示例：启动一个 HTTP 服务

```go
package main

import (
	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/app"
	kithttp "github.com/kochabx/kit/transport/http"
)

func main() {
	engine := gin.Default()
	engine.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	application := app.New(
		app.WithServer(kithttp.NewServer(":8080", engine)),
	)

	if err := application.Start(); err != nil {
		panic(err)
	}
}
```

### 生产化示例：配置、日志、数据库、Redis、HTTP 服务一起启动

```go
package main

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/app"
	"github.com/kochabx/kit/config"
	"github.com/kochabx/kit/ioc"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/store/db"
	"github.com/kochabx/kit/store/redis"
	kithttp "github.com/kochabx/kit/transport/http"
)

type Config struct {
	DB struct {
		Host     string `json:"host" default:"localhost"`
		Port     int    `json:"port" default:"3306"`
		User     string `json:"user" default:"root"`
		Password string `json:"password"`
		Database string `json:"database" default:"mydb"`
	} `json:"db"`

	Redis struct {
		Host     string `json:"host" default:"localhost"`
		Port     int    `json:"port" default:"6379"`
		Password string `json:"password"`
	} `json:"redis"`
}

func main() {
	cfg := &Config{}
	if err := config.New(cfg).Load(); err != nil {
		panic(err)
	}

	logger := log.New()
	log.SetGlobal(logger)

	container := ioc.NewApplicationContainer()
	_ = container

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

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	httpServer := kithttp.NewServer(":8080", engine)

	application := app.New(
		app.WithServer(httpServer),
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

## 详细文档

### 应用层能力

#### [app](app/)

- 管理多个服务的统一启动与关闭
- 统一接收系统信号
- 支持资源注册与按顺序清理
- 支持关闭超时控制

#### [config](config/README.md)

- 支持 YAML、JSON、TOML 等配置格式
- 支持环境变量覆盖
- 支持结构体绑定、默认值与校验
- 支持配置热更新

#### [ioc](ioc/README.md)

- 类型安全的依赖注入
- 多命名空间管理
- 生命周期控制
- 便于模块化注册与测试替换

### 网络与通信

#### [transport/http](transport/http/)

- 基于 Gin 的 HTTP 服务封装
- 便于接入路由、健康检查和业务中间件
- 适合作为服务启动入口与应用生命周期集成

#### [transport/http/middleware](transport/http/middleware/README.md)

- 认证、日志、恢复、签名验证、XSS 防护、CORS 等中间件
- 便于构建统一的 HTTP 接入层

#### [transport/grpc](transport/grpc/)

- gRPC 服务启动封装
- 适合统一接入生命周期管理

#### [transport/grpc/middleware](transport/grpc/middleware/README.md)

- gRPC 中间件支持

#### [transport/websocket](transport/websocket/README.md)

- 自动重连
- 心跳保活
- 事件驱动
- 消息队列与连接状态管理

#### [core/httpx](core/httpx/)

- HTTP 客户端工具
- 适合统一处理请求构建、响应读取与内容协商

### 存储能力

#### [store/db](store/db/)

- GORM 封装
- 支持 MySQL、PostgreSQL、SQLite
- 统一连接配置与管理

#### [store/redis](store/redis/README.md)

- 支持单机与集群模式
- 支持连接池优化与业务常用配置

#### [store/mongo](store/mongo/)

- MongoDB 客户端封装

#### [store/etcd](store/etcd/)

- Etcd 客户端封装与辅助工具

#### [store/kafka](store/kafka/)

- Kafka 生产与消费能力

#### [store/oss/minio](store/oss/minio/README.md)

- MinIO 对象存储客户端
- 支持分片上传与断点续传

### 分布式与通用组件

#### [core/scheduler](core/scheduler/README.md)

- 延迟任务、Cron 任务、优先级队列
- 分布式锁、重试、去重、死信队列
- 基于 Redis 的分布式任务调度能力

#### [core/rate](core/rate/)

- 令牌桶限流
- 滑动窗口限流
- Lua 脚本支持

#### [log](log/README.md)

- 基于 Zerolog 的结构化日志
- 支持敏感数据脱敏
- 支持日志轮转
- 支持全局日志器与自定义 writer

#### [observability/metrics](observability/metrics/)

- Prometheus 指标采集
- 适合与 HTTP、gRPC 统一接入

#### [core/validator](core/validator/)

- 多语言校验错误输出
- 支持扩展自定义规则

#### [errors](errors/)

- 结构化错误定义
- 错误包装、透传与错误链处理
- 便于统一错误响应与日志记录

#### [core/defaults](core/defaults/)

- 基于结构体标签的默认值注入
- 支持嵌套结构、切片、Map、指针标量等场景
- 支持自定义解析器与字段过滤

#### [core/util](core/util/)

- 上下文工具
- 类型转换
- ID 生成
- 二维码、树结构等辅助能力

## 设计理念

### 模块化

每个模块都可以单独使用。你可以只引入日志、配置、JWT 或调度器，而不必接受整个框架体系。

### 类型安全

优先使用 Go 原生类型系统和泛型能力，在编译期暴露问题，减少运行期隐患。

### 性能优先

- 尽量减少不必要分配
- 在热点路径中进行缓存和复用
- 保持并发安全与实现简洁之间的平衡

### 生产可用

- 统一错误模型
- 结构化日志
- 指标与可观测性支持
- 生命周期和优雅关闭能力

### 易于测试

- 接口与实现边界清晰
- 组件可单独使用和替换
- 方便在测试环境做 mock 或裁剪依赖

## 项目结构

```text
kit/
├── app/                     # 应用生命周期管理
├── config/                  # 配置管理
├── errors/                  # 错误定义与包装
├── ioc/                     # 依赖注入容器
├── log/                     # 日志系统
├── observability/
│   └── metrics/             # 指标采集
├── store/
│   ├── db/                  # 数据库
│   ├── etcd/                # Etcd
│   ├── kafka/               # Kafka
│   ├── mongo/               # MongoDB
│   ├── oss/                 # 对象存储
│   └── redis/               # Redis
├── transport/
│   ├── grpc/                # gRPC 服务与中间件
│   ├── http/                # HTTP 服务与中间件
│   └── websocket/           # WebSocket 客户端
└── core/
    ├── auth/                # JWT、MFA
    ├── crypto/              # ECIES、HMAC
    ├── defaults/            # 默认值注入
    ├── httpx/               # HTTP 客户端
    ├── rate/                # 限流
    ├── scheduler/           # 分布式调度
    ├── util/                # 工具集
    └── validator/           # 参数校验
```

## 开发命令

项目提供了 Makefile 以简化日常开发操作。

### 代码质量

```bash
make fmt
make vet
```

### 依赖与工具

```bash
make install
make upgrade
```

### 代码生成

```bash
make proto
make wire
make swag
```

### 其他命令

```bash
make info
make help
```

## 使用建议

如果你是第一次接入这个项目，建议按下面的顺序了解：

1. 先看 [app](app/) 和 [config](config/README.md)，理解应用启动与配置模型
2. 再看 [log](log/README.md)、[errors](errors/) 和 [core/defaults](core/defaults/)，建立基础工程规范
3. 根据你的服务类型选择 [transport/http](transport/http/) 或 [transport/grpc](transport/grpc/)
4. 按需接入 [store/db](store/db/)、[store/redis](store/redis/README.md)、[core/scheduler](core/scheduler/README.md) 等基础设施组件

## 许可证

本项目基于 MIT License 发布。