# Kit - 企业级 Go 微服务工具包

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/kochabx/kit)](https://goreportcard.com/report/github.com/kochabx/kit)

**Kit** 是一个功能完备、生产就绪的 Go 微服务工具包，提供了构建现代化分布式系统所需的全套基础组件。采用模块化设计，遵循 Go 最佳实践，助力快速构建高性能、高可靠的企业级应用。

## ✨ 核心特性

### 🏗️ 应用框架层
- **[App](app/)** - 优雅的应用生命周期管理，支持多服务器运行、信号处理和优雅关闭
- **[IoC](ioc/)** - 轻量级类型安全的依赖注入容器，支持多命名空间和生命周期管理
- **[Config](config/)** - 基于 Viper 的智能配置管理，支持热加载、环境变量覆盖和结构验证

### 🔐 安全认证
- **[JWT](core/auth/jwt/)** - 完整的 JWT 认证方案，支持 Token 刷新、JTI 管理和 Redis 缓存
- **[MFA](core/auth/mfa/)** - Google Authenticator TOTP 多因子认证
- **[ECIES](core/crypto/ecies/)** - 基于椭圆曲线的集成加密方案（P-256 + AES-256-GCM）
- **[HMAC](core/crypto/hmac/)** - 消息认证码，用于签名验证和数据完整性校验

### 🗄️ 数据存储
- **[GORM](store/db/)** - 支持 MySQL/PostgreSQL/SQLite 的 ORM 封装，含连接池管理
- **[Redis](store/redis/)** - 单机和集群模式支持，优化的连接池配置
- **[MongoDB](store/mongo/)** - MongoDB 客户端封装
- **[Etcd](store/etcd/)** - 分布式配置和服务发现
- **[Kafka](store/kafka/)** - 消息队列生产者/消费者

### 🌐 网络传输
- **[HTTP Server](transport/http/)** - 基于 Gin 的高性能 HTTP 服务器，集成 Swagger、Prometheus、健康检查
- **[gRPC Server](transport/grpc/)** - gRPC 服务器封装
- **[WebSocket](transport/websocket/)** - 功能完备的 WebSocket 客户端：自动重连、心跳检测、消息队列
- **[HTTP Client](core/httpclient/)** - 优化的 HTTP 客户端，含对象池和自动内容协商

### 🎯 分布式特性
- **[Scheduler](core/scheduler/)** - 基于 Redis Stream 的分布式任务调度系统
  - 延迟任务、Cron 周期任务、优先级队列
  - 分布式锁、故障转移、死信队列
  - 完全类型安全的泛型 API
- **[Rate Limiter](core/rate/)** - 高性能限流器（令牌桶、滑动窗口算法）
- **[OSS](core/oss/minio/)** - MinIO 对象存储客户端，支持分片上传和断点续传

### 📊 可观测性
- **[Logger](log/)** - 基于 Zerolog 的高性能日志库，支持数据脱敏和日志轮转
- **[Metrics](metrics/)** - Prometheus 指标采集（HTTP/gRPC 中间件集成）
- **[Middleware](middleware/)** - 丰富的 HTTP/gRPC 中间件：认证、日志、限流、恢复、CORS 等

### 🛠️ 核心工具
- **[Validator](core/validator/)** - 基于 validator.v10 的多语言验证器（支持中英文）
- **[Errors](errors/)** - 结构化错误处理，支持错误链和元数据
- **[Tag Parser](core/tag/)** - 智能结构体标签解析器，支持默认值注入
- **[Utilities](core/util/)** - 上下文工具、类型转换、ID 生成、二维码、树形结构等

## 🚀 快速开始

### 安装

```bash
go get github.com/kochabx/kit
```

### 最小化示例 - 创建 HTTP 服务

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/kochabx/kit/app"
    "github.com/kochabx/kit/transport/http"
)

func main() {
    // 创建 Gin 路由
    engine := gin.Default()
    engine.GET("/ping", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "pong"})
    })

    // 创建应用并启动
    application := app.New(
        app.WithServer(http.NewServer(":8080", engine)),
    )

    if err := application.Start(); err != nil {
        panic(err)
    }
}
```

### 完整示例 - 生产级应用

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
    "github.com/kochabx/kit/transport/http"
)

func main() {
    // 1. 初始化配置
    cfg := &Config{}
    if err := config.New(cfg).Load(); err != nil {
        panic(err)
    }

    // 2. 初始化日志
    logger := log.New()
    log.SetGlobal(logger)

    // 3. 创建 IoC 容器
    container := ioc.NewApplicationContainer()

    // 4. 初始化数据库
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

    // 5. 初始化 Redis
    rdb, err := redis.NewClient(&redis.SingleConfig{
        Host:     cfg.Redis.Host,
        Port:     cfg.Redis.Port,
        Password: cfg.Redis.Password,
    })
    if err != nil {
        panic(err)
    }

    // 6. 创建 HTTP 服务器
    engine := gin.New()
    engine.Use(gin.Recovery())
    
    // 注册路由
    engine.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })

    httpServer := http.NewServer(":8080", engine)

    // 7. 创建应用并配置资源清理
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

    // 8. 启动应用
    log.Info().Msg("Starting application...")
    if err := application.Start(); err != nil {
        log.Fatal().Err(err).Msg("Failed to start application")
    }
}

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
```

## 📖 详细文档

### 核心模块

#### 1. 应用框架

- **[App 模块](app/)** - 应用生命周期管理
  - 多服务器并发运行
  - 优雅关闭和资源清理
  - 信号处理和超时控制

- **[IoC 容器](ioc/)** - 依赖注入容器 ([查看文档](ioc/README.md))
  - 类型安全的组件注册
  - 多命名空间管理
  - 自动依赖注入和初始化
  - 健康检查和度量

- **[Config 配置](config/)** - 配置管理 ([查看文档](config/README.md))
  - 支持 YAML/JSON/TOML
  - 环境变量自动覆盖
  - 热加载和验证
  - 默认值和类型安全

#### 2. 网络与传输

- **[HTTP Server](transport/http/)** - HTTP 服务器
  - 基于 Gin 框架
  - 集成 Swagger 文档
  - Prometheus 指标采集
  - 健康检查端点

- **[gRPC Server](transport/grpc/)** - gRPC 服务器
  - 协议缓冲区支持
  - 流式 RPC
  - 拦截器链

- **[WebSocket Client](transport/websocket/)** - WebSocket 客户端
  - 自动重连机制
  - 心跳保活
  - 事件驱动架构
  - 消息队列

- **[HTTP Client](core/httpclient/)** - HTTP 客户端
  - 对象池优化
  - 请求/响应拦截器
  - 自动序列化/反序列化

#### 3. 数据存储

- **[GORM](store/db/)** - ORM 数据库
  - MySQL, PostgreSQL, SQLite
  - 连接池管理
  - 事务支持

- **[Redis](store/redis/)** - Redis 客户端
  - 单机/集群模式
  - 管道和批量操作
  - 连接池优化

- **[MongoDB](store/mongo/)** - MongoDB 客户端
- **[Etcd](store/etcd/)** - Etcd 客户端  
- **[Kafka](store/kafka/)** - Kafka 客户端

#### 4. 分布式系统

- **[Scheduler](core/scheduler/)** - 分布式任务调度 ([查看文档](core/scheduler/README.md))
  - 延迟任务和 Cron 任务
  - 优先级队列
  - 分布式锁和去重
  - 失败重试和死信队列
  - 完全类型安全的泛型 API

- **[Rate Limiter](core/rate/)** - 限流器
  - 令牌桶算法
  - 滑动窗口算法
  - 基于 Redis Lua 脚本

#### 5. 安全认证

- **[JWT](core/auth/jwt/)** - JWT 认证
  - Token 生成和验证
  - 刷新 Token 支持
  - Redis 缓存集成
  - JTI 管理

- **[MFA](core/auth/mfa/)** - 多因子认证
  - Google Authenticator
  - TOTP 时间同步

- **[ECIES](core/crypto/ecies/)** - 椭圆曲线加密
  - NIST P-256 曲线
  - AES-256-GCM 认证加密
  - 密钥持久化

- **[HMAC](core/crypto/hmac/)** - 消息认证码

#### 6. 可观测性

- **[Logger](log/)** - 日志系统 ([查看文档](log/README.md))
  - 基于 Zerolog 高性能
  - 结构化日志
  - 数据脱敏
  - 日志轮转（按时间/大小）

- **[Metrics](metrics/)** - 指标采集
  - Prometheus 集成
  - HTTP/gRPC 指标
  - 自定义指标

#### 7. 中间件

- **[HTTP Middleware](middleware/http/)** - HTTP 中间件
  - 认证 (Auth)
  - CORS 跨域
  - 加密/解密
  - 日志记录
  - 权限检查
  - 恢复 (Recovery)
  - 签名验证
  - XSS 防护

- **[gRPC Middleware](middleware/grpc/)** - gRPC 中间件

#### 8. 工具库

- **[Validator](core/validator/)** - 数据验证
  - 多语言支持（中文/英文）
  - 自定义验证规则
  - 详细错误信息

- **[Errors](errors/)** - 错误处理
  - 结构化错误
  - 错误链和元数据
  - HTTP 状态码映射

- **[Tag Parser](core/tag/)** - 标签解析 ([查看文档](core/tag/README.md))
  - 结构体默认值注入
  - 嵌套结构支持
  - 自定义解析器

- **[OSS](core/oss/minio/)** - 对象存储
  - MinIO 客户端
  - 分片上传
  - 断点续传

- **[Utilities](core/util/)** - 实用工具
  - 上下文辅助函数
  - 类型转换
  - UUID/雪花 ID 生成
  - 二维码生成
  - 树形结构处理

## 🎯 设计理念

### 模块化设计
每个模块都是独立的，可以单独使用，也可以组合使用。遵循单一职责原则，保持代码清晰和可维护。

### 类型安全
充分利用 Go 泛型（Go 1.18+），提供编译时类型检查，减少运行时错误。

### 性能优先
- 零分配或少分配设计
- 对象池复用
- 并发安全的高性能实现

### 生产就绪
- 完整的错误处理
- 结构化日志
- 健康检查和指标
- 优雅关闭和资源清理

### 易于测试
- 接口驱动设计
- 依赖注入
- Mock 友好

## 🏗️ 项目结构

```
kit/
├── app/              # 应用框架
├── config/           # 配置管理
├── ioc/              # IoC 容器
├── log/              # 日志系统
├── errors/           # 错误处理
├── metrics/          # 指标采集
├── middleware/       # 中间件
│   ├── http/        # HTTP 中间件
│   └── grpc/        # gRPC 中间件
├── transport/        # 传输层
│   ├── http/        # HTTP 服务器
│   ├── grpc/        # gRPC 服务器
│   └── websocket/   # WebSocket 客户端
├── store/            # 存储层
│   ├── db/          # 数据库（GORM）
│   ├── redis/       # Redis
│   ├── mongo/       # MongoDB
│   ├── etcd/        # Etcd
│   └── kafka/       # Kafka
└── core/             # 核心工具
    ├── auth/        # 认证（JWT, MFA）
    ├── crypto/      # 加密（ECIES, HMAC）
    ├── httpclient/  # HTTP 客户端
    ├── oss/         # 对象存储（MinIO）
    ├── rate/        # 限流器
    ├── scheduler/   # 任务调度器
    ├── tag/         # 标签解析器
    ├── util/        # 实用工具
    └── validator/   # 数据验证器
```

## 🔧 开发工具

### Makefile 命令

```bash
# 代码质量
make fmt          # 格式化代码
make vet          # 静态分析
make lint         # Lint 检查

# 测试
make test         # 运行测试
make coverage     # 生成覆盖率报告

# 代码生成
make proto        # 生成 gRPC 代码
make wire         # 生成 Wire 依赖注入代码
make swag         # 生成 Swagger 文档

# 依赖管理
make install      # 安装开发工具
make upgrade      # 升级依赖
make mod-tidy     # 整理依赖

# 安全
make security     # 安全扫描
```

## 📦 依赖版本

- **Go**: 1.25.0+
- **Gin**: v1.11.0
- **Zerolog**: latest
- **GORM**: v1.25+
- **Redis**: go-redis/v9
- **Viper**: v1.19+
- **Prometheus**: client_golang

## 🤝 贡献指南

欢迎贡献代码！请遵循以下步骤：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

### 代码规范

- 遵循 [Effective Go](https://go.dev/doc/effective_go)
- 使用 `gofmt` 格式化代码
- 保持测试覆盖率 > 80%
- 添加必要的文档注释

---
result, err := client.InitiateMultipartUpload(ctx, params)
```

#### HTTP中间件

```go
import "github.com/kochabx/kit/middleware/http"

engine.Use(http.Logger())
engine.Use(http.Recovery())
engine.Use(http.CORS())
engine.Use(http.Auth())
engine.Use(http.XSS())
```

#### JWT认证

```go
import "github.com/kochabx/kit/core/auth/jwt"

jwtManager := jwt.New(jwt.Config{
    Secret: "your-secret-key",
    Expire: time.Hour * 24,
})

token, err := jwtManager.GenerateToken("user123", map[string]any{
    "role": "admin",
})
```

#### Redis存储

```go
import "github.com/kochabx/kit/store/redis"

client := redis.New(redis.Config{
    Addr: "localhost:6379",
    DB:   0,
})
```
