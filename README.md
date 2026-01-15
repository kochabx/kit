# Kit - Go微服务工具包

Kit是一个功能丰富的Go语言微服务工具包，提供了构建生产级微服务所需的各种组件和工具。

## 项目特性

- 🚀 **应用框架**: 优雅的服务生命周期管理，支持多服务器运行和优雅关闭
- 🏗️ **IoC容器**: 轻量级依赖注入，支持多命名空间和生命周期管理
- ⚙️ **配置管理**: 基于Viper，支持热加载、环境变量覆盖和配置验证
- 🔐 **认证授权**: JWT、MFA多因子认证支持
- 🔒 **加密算法**: ECIES、HMAC等加密工具
- 📊 **监控指标**: Prometheus集成，完整的可观测性支持
- 🗄️ **存储支持**: GORM、Redis、MongoDB、Etcd、Kafka
- ⚡ **限流器**: 令牌桶、滑动窗口算法
- 🌐 **HTTP/gRPC**: 基于Gin的HTTP服务器和gRPC支持
- 🔌 **WebSocket**: 功能丰富的WebSocket客户端，支持自动重连和心跳
- 📝 **日志系统**: 结构化日志与脱敏功能
- 🔍 **参数验证**: 通用验证器支持
- 🎯 **任务调度**: 基于Redis的分布式任务调度器，支持Cron任务、延迟任务
- 📦 **对象存储**: MinIO客户端，支持分片上传和断点续传
- 🛠️ **工具集**: 上下文工具、类型转换、标签解析等实用工具

## 快速开始

### 安装

```bash
go get github.com/kochabx/kit
```

### App模块 - 创建和管理服务

App模块是Kit工具包的核心，提供了完整的应用生命周期管理，支持多服务器运行、优雅关闭和资源清理。

#### 基本使用

```go
package main

import (
    "context"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/kochabx/kit/app"
    "github.com/kochabx/kit/transport/http"
)

func main() {
    // 创建Gin引擎
    engine := gin.New()
    engine.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })

    // 创建HTTP服务器
    httpServer := http.NewServer(":8080", engine)

    // 创建应用实例
    application := app.New(
        app.WithServer(httpServer),
        app.WithShutdownTimeout(30*time.Second),
    )

    // 启动应用
    if err := application.Start(); err != nil {
        panic(err)
    }
}
```

#### 高级配置

```go
package main

import (
    "context"
    "database/sql"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/kochabx/kit/app"
    "github.com/kochabx/kit/transport/http"
)

func main() {
    // 创建多个服务
    adminEngine := gin.New()
    adminEngine.GET("/admin/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"service": "admin"})
    })
    adminServer := http.NewServer(":8081", adminEngine)

    apiEngine := gin.New()
    apiEngine.GET("/api/v1/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"service": "api"})
    })
    apiServer := http.NewServer(":8080", apiEngine)

    // 模拟数据库连接
    var db *sql.DB // 实际项目中需要初始化

    // 创建应用实例，支持多服务器和资源清理
    application := app.New(
        // 添加多个服务器
        app.WithServers(adminServer, apiServer),
        
        // 设置自定义上下文
        app.WithContext(context.Background()),
        
        // 配置关闭超时
        app.WithShutdownTimeout(30*time.Second),
        app.WithCloseTimeout(10*time.Second),
        
        // 添加资源清理函数
        app.WithClose("database", func(ctx context.Context) error {
            if db != nil {
                return db.Close()
            }
            return nil
        }, 5*time.Second),
        
        app.WithClose("cache", func(ctx context.Context) error {
            // 清理缓存逻辑
            return nil
        }, 3*time.Second),
    )

    // 运行时添加服务器
    metricsEngine := gin.New()
    metricsEngine.GET("/metrics", func(c *gin.Context) {
        c.String(200, "metrics data")
    })
    metricsServer := http.NewServer(":9090", metricsEngine)
    
    if err := application.AddServer(metricsServer); err != nil {
        panic(err)
    }

    // 运行时添加清理函数
    if err := application.RegisterClose("metrics", func(ctx context.Context) error {
        // 清理指标收集器
        return nil
    }, 2*time.Second); err != nil {
        panic(err)
    }

    // 启动应用
    if err := application.Start(); err != nil {
        panic(err)
    }
}
```

#### App模块特性

- **多服务器支持**: 同时运行多个HTTP/gRPC服务器
- **优雅关闭**: 接收系统信号自动优雅关闭所有服务
- **资源清理**: 支持注册清理函数，确保资源正确释放
- **超时控制**: 可配置服务关闭和清理函数的超时时间
- **并发安全**: 线程安全的服务器和清理函数管理
- **错误处理**: 完整的错误处理和日志记录

#### 配置选项

| 选项 | 说明 | 默认值 |
|------|------|--------|
| `WithContext` | 设置应用根上下文 | `context.Background()` |
| `WithServer` | 添加单个服务器 | - |
| `WithServers` | 添加多个服务器 | - |
| `WithShutdownTimeout` | 设置服务关闭超时时间 | `30s` |
| `WithCloseTimeout` | 设置清理函数默认超时时间 | `10s` |
| `WithSignals` | 设置自定义关闭信号 | `SIGINT, SIGTERM, SIGQUIT` |
| `WithClose` | 添加资源清理函数 | - |

#### 运行时管理

```go
// 获取应用信息
info := application.Info()
fmt.Printf("服务器数量: %d\n", info.ServerCount)
fmt.Printf("清理函数数量: %d\n", info.CleanupCount)
fmt.Printf("是否已启动: %t\n", info.Started)

// 手动停止应用
application.Stop()
```

### 其他模块示例

#### 分布式任务调度器

```go
import "github.com/kochabx/kit/core/scheduler"

// 创建调度器
s, err := scheduler.New(
    scheduler.WithRedisAddr("localhost:6379"),
    scheduler.WithNamespace("myapp"),
    scheduler.WithWorkerCount(10),
)

// 注册任务处理器
type EmailPayload struct {
    To      string `json:"to"`
    Subject string `json:"subject"`
}

s.RegisterHandler("send_email", scheduler.HandlerFunc[EmailPayload](
    func(ctx context.Context, task *scheduler.Task[EmailPayload]) error {
        // 处理邮件发送
        return nil
    },
))

// 提交任务
s.Submit(ctx, "send_email", EmailPayload{
    To:      "user@example.com",
    Subject: "Hello",
}, scheduler.WithPriority(scheduler.PriorityHigh))

// 提交Cron任务
s.SubmitCron(ctx, "daily_report", "0 0 * * *", ReportPayload{})
```

#### IoC容器

```go
import "github.com/kochabx/kit/ioc"

// 创建应用容器
container := ioc.NewApplicationContainer()

// 注册组件
container.RegisterConfig(&MyConfigComponent{})
container.RegisterDatabase(&MyDatabaseComponent{})

// 初始化
ctx := context.Background()
container.Initialize(ctx)

// 获取组件
config := container.GetConfig("myConfig")
db := container.GetDatabase("myDatabase")
```

#### 配置管理

```go
import "github.com/kochabx/kit/config"

type AppConfig struct {
    Server struct {
        Host string `json:"host" default:"localhost"`
        Port int    `json:"port" default:"8080" validate:"min=1,max=65535"`
    } `json:"server"`
}

cfg := &AppConfig{}
c := config.New(cfg)
c.Load()

// 启动热加载
c.Watch()
```

#### WebSocket客户端

```go
import "github.com/kochabx/kit/core/net/websocket"

client := websocket.NewClient()

client.OnEvent(websocket.EventMessage, func(event websocket.Event) {
    if msg, ok := event.Data.(websocket.Message); ok {
        log.Printf("收到消息: %s", string(msg.Data))
    }
})

ctx := context.Background()
client.Connect(ctx, "wss://echo.websocket.org")
client.SendText("Hello WebSocket!")
```

#### MinIO对象存储

```go
import "github.com/kochabx/kit/core/oss/minio"

client, err := minio.NewClient(
    "localhost:9000",
    "access-key",
    "secret-key",
    minio.WithUseSSL(false),
)

// 创建桶
client.CreateBucket(ctx, "my-bucket")

// 分片上传
params := &minio.InitiateMultipartParams{
    Bucket:     "my-bucket",
    Object:     "large-file.bin",
    ObjectSize: 100 * 1024 * 1024,
    PartSize:   10 * 1024 * 1024,
}
result, err := client.InitiateMultipartUpload(ctx, params)
```

#### HTTP中间件

```go
import "github.com/kochabx/kit/transport/http/middleware"

engine.Use(middleware.Logger())
engine.Use(middleware.Recovery())
engine.Use(middleware.CORS())
engine.Use(middleware.Auth())
engine.Use(middleware.XSS())
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

## 项目结构

```
├── app/              # 应用框架和生命周期管理
├── config/           # 配置管理（基于Viper，支持热加载）
├── core/            # 核心功能组件
│   ├── auth/        # 认证相关（JWT、MFA）
│   │   ├── jwt/     # JWT令牌管理
│   │   └── mfa/     # 多因子认证（Google Authenticator）
│   ├── crypto/      # 加密算法
│   │   ├── ecies/   # ECIES椭圆曲线加密
│   │   └── hmac/    # HMAC签名算法
│   ├── net/         # 网络工具
│   │   ├── http/    # HTTP工具
│   │   └── websocket/ # WebSocket客户端
│   ├── oss/         # 对象存储
│   │   └── minio/   # MinIO客户端
│   ├── rate/        # 限流器（令牌桶、滑动窗口）
│   ├── scheduler/   # 分布式任务调度器
│   ├── stag/        # 结构体标签解析
│   ├── util/        # 工具函数
│   │   ├── convert/ # 类型转换
│   │   └── tree/    # 树形结构
│   └── validator/   # 参数验证
├── errors/          # 错误处理
├── ioc/             # IoC依赖注入容器
├── log/             # 日志系统（结构化日志、脱敏）
│   └── desensitize/ # 数据脱敏
├── store/           # 存储适配器
│   ├── db/          # 数据库（GORM、Ent）
│   ├── redis/       # Redis客户端
│   ├── mongo/       # MongoDB客户端
│   ├── etcd/        # Etcd客户端
│   └── kafka/       # Kafka客户端
└── transport/       # 传输层
    ├── http/        # HTTP服务器（Gin）
    │   ├── middleware/ # 中间件（日志、认证、CORS等）
    │   ├── metrics/    # Prometheus指标
    │   └── response/   # 统一响应格式
    └── grpc/        # gRPC服务器
```

## 核心模块说明

### 🚀 App - 应用框架
优雅的应用生命周期管理，支持多服务器运行、优雅关闭和资源清理。

**主要特性：**
- 多服务器支持（HTTP/gRPC）
- 自动信号监听和优雅关闭
- 资源清理函数注册
- 超时控制
- 并发安全

[查看详细文档](app/)

### 🏗️ IoC - 依赖注入容器
轻量级、类型安全的依赖注入容器，无反射设计。

**主要特性：**
- 多命名空间管理
- 组件生命周期管理
- 依赖注入和解析
- 健康检查支持
- Gin路由自动注册

[查看详细文档](ioc/)

### ⚙️ Config - 配置管理
基于Viper的配置管理，支持多种格式和热加载。

**主要特性：**
- YAML/JSON/TOML支持
- 环境变量自动覆盖
- 配置热加载
- 配置验证
- 并发安全

[查看详细文档](config/)

### 🎯 Scheduler - 分布式任务调度器
基于Redis的高性能分布式任务调度系统。

**主要特性：**
- 纯泛型设计，类型安全
- 延迟任务、Cron任务、立即任务
- 优先级队列（高/中/低）
- 分布式锁和去重
- 失败重试和死信队列
- 限流和熔断保护
- Prometheus监控

[查看详细文档](core/scheduler/)

### 🔌 WebSocket - WebSocket客户端
功能丰富的WebSocket客户端库。

**主要特性：**
- 自动重连（指数退避）
- 事件驱动架构
- Ping/Pong心跳检测
- 并发安全
- TLS/WSS支持
- 灵活配置

[查看详细文档](core/net/websocket/)

### 📦 MinIO - 对象存储客户端
生产级MinIO对象存储客户端。

**主要特性：**
- 桶管理操作
- 预签名URL上传
- 分片上传支持
- 断点续传
- 并发控制
- 完善的错误处理

[查看详细文档](core/oss/minio/)

### 🔐 Auth - 认证授权
JWT令牌管理和多因子认证支持。

**JWT特性：**
- 令牌生成和验证
- Token缓存
- 刷新令牌支持

**MFA特性：**
- Google Authenticator
- TOTP验证

[查看详细文档](core/auth/)

### ⚡ Rate - 限流器
高性能限流器实现。

**支持算法：**
- 令牌桶算法
- 滑动窗口算法
- 基于Redis的分布式限流

[查看详细文档](core/rate/)

### 🗄️ Store - 存储适配器
统一的存储层抽象。

**支持的存储：**
- **数据库**: GORM、Ent
- **缓存**: Redis
- **NoSQL**: MongoDB
- **配置中心**: Etcd
- **消息队列**: Kafka

### 🌐 Transport - 传输层
HTTP和gRPC服务器支持。

**HTTP特性：**
- 基于Gin框架
- 丰富的中间件（认证、日志、CORS、XSS、限流等）
- Prometheus指标采集
- 统一响应格式

**gRPC特性：**
- 标准gRPC服务器
- 地址验证
- 优雅关闭

### 📝 Log - 日志系统
结构化日志和数据脱敏。

**主要特性：**
- 结构化日志输出
- 数据脱敏（手机号、邮箱、身份证等）
- 日志轮转
- 多种输出方式（控制台、文件）
- 高性能

[查看详细文档](log/)

### 🔍 Validator - 参数验证
基于go-playground/validator的验证器封装。

**主要特性：**
- 结构体验证
- 自定义验证规则
- 国际化错误消息
- 友好的错误提示

### 🛠️ Util - 工具集
实用工具函数集合。

**包含工具：**
- **Context**: 上下文工具函数
- **Convert**: 类型转换工具
- **Tree**: 树形结构处理
- **Stag**: 结构体标签解析
