# IoC (Inversion of Control) 模块

IoC 模块提供了一个轻量级、类型安全的依赖注入容器，支持多命名空间管理、生命周期管理和健康检查等功能。该模块遵循接口隔离原则，不使用反射，提供高性能的依赖注入解决方案。

## 主要特性

- **类型安全**：编译时类型检查
- **多命名空间**：支持组件的分层管理
- **生命周期管理**：完整的组件初始化和销毁流程
- **依赖注入**：简单高效的依赖管理
- **健康检查**：内置健康状态监控
- **容器度量**：提供详细的容器运行指标
- **优雅关闭**：支持超时控制的优雅关闭

## 核心接口

### Component
```go
type Component interface {
    Name() string
}
```
所有组件都必须实现的基础接口，提供唯一标识符。

### 可选接口

```go
// 初始化接口
type Initializer interface {
    Init(ctx context.Context) error
}

// 销毁接口
type Destroyer interface {
    Destroy(ctx context.Context) error
}

// 健康检查接口
type HealthChecker interface {
    HealthCheck(ctx context.Context) error
}

// 排序接口
type Orderable interface {
    Order() int
}

// 依赖注入接口
type DependencyInjector interface {
    InjectDependencies(injector DependencyResolver) error
}

// Gin 路由注册接口
type GinRouter interface {
    Component
    RegisterGinRoutes(r gin.IRouter) error
}
```

## 默认命名空间

模块预定义了以下命名空间（按初始化顺序）：

- `config` (优先级: -100) - 配置组件
- `database` (优先级: -80) - 数据库组件  
- `baseComponent` (优先级: -60) - 基础组件
- `handler` (优先级: -40) - 处理器组件
- `controller` (优先级: -20) - 控制器组件

## 快速开始

### 基本使用

```go
// 创建应用容器
container := ioc.NewApplicationContainer()

// 注册配置组件
err := container.RegisterConfig(&MyConfigComponent{})
if err != nil {
    log.Fatal(err)
}

// 注册数据库组件
err = container.RegisterDatabase(&MyDatabaseComponent{})
if err != nil {
    log.Fatal(err)
}

// 初始化容器
ctx := context.Background()
err = container.Initialize(ctx)
if err != nil {
    log.Fatal(err)
}

// 获取组件
config := container.GetConfig("myConfig")
db := container.GetDatabase("myDatabase")

// 应用结束时关闭容器
defer container.Shutdown(ctx)
```

### 自定义配置

```go
// 使用自定义配置创建容器
container := ioc.NewApplicationContainer(
    ioc.WithApplicationVerbose(),
    ioc.WithApplicationShutdownTimeout(60*time.Second),
    ioc.WithApplicationNamespace("custom", -10),
)
```

### 使用构建器模式

```go
// 创建自定义构建器
builder := ioc.NewContainerBuilder().
    WithDefaultNamespaces().
    AddNamespace("custom", -10).
    WithShutdownTimeout(60*time.Second).
    WithVerbose(true).
    AddBeforeInitHook(func(ctx context.Context, store *ioc.Store) error {
        log.Info("容器开始初始化")
        return nil
    })

// 构建容器
store, lifecycle := builder.Build()
container := ioc.NewCustomApplicationContainer(builder)
```

## 组件示例

### 基础组件

```go
type MyComponent struct {
    name string
}

func (c *MyComponent) Name() string {
    return c.name
}

func (c *MyComponent) Init(ctx context.Context) error {
    log.Info("组件初始化: %s", c.name)
    return nil
}

func (c *MyComponent) Destroy(ctx context.Context) error {
    log.Info("组件销毁: %s", c.name)
    return nil
}

func (c *MyComponent) HealthCheck(ctx context.Context) error {
    // 执行健康检查逻辑
    return nil
}
```

### 有序组件

```go
type OrderedComponent struct {
    MyComponent
    order int
}

func (c *OrderedComponent) Order() int {
    return c.order
}
```

### 依赖注入组件

```go
type ServiceComponent struct {
    name string
    db   *DatabaseComponent
}

func (s *ServiceComponent) Name() string {
    return s.name
}

func (s *ServiceComponent) InjectDependencies(injector ioc.DependencyResolver) error {
    db, err := injector.ResolveDependency("database")
    if err != nil {
        return err
    }
    s.db = db.(*DatabaseComponent)
    return nil
}
```

### Gin 路由组件

```go
type UserController struct {
    name string
}

func (uc *UserController) Name() string {
    return uc.name
}

func (uc *UserController) RegisterGinRoutes(r gin.IRouter) error {
    userGroup := r.Group("/api/users")
    {
        userGroup.GET("", uc.GetUsers)
        userGroup.GET("/:id", uc.GetUser)
        userGroup.POST("", uc.CreateUser)
        userGroup.PUT("/:id", uc.UpdateUser)
        userGroup.DELETE("/:id", uc.DeleteUser)
    }
    return nil
}

func (uc *UserController) GetUsers(c *gin.Context) {
    // 获取用户列表的实现
}

func (uc *UserController) GetUser(c *gin.Context) {
    // 获取单个用户的实现
}

func (uc *UserController) CreateUser(c *gin.Context) {
    // 创建用户的实现
}

func (uc *UserController) UpdateUser(c *gin.Context) {
    // 更新用户的实现
}

func (uc *UserController) DeleteUser(c *gin.Context) {
    // 删除用户的实现
}
```

## 生命周期管理

### 生命周期状态

- `StateNew` - 新创建，未初始化
- `StateInitializing` - 正在初始化
- `StateRunning` - 运行中
- `StateShutting` - 正在关闭
- `StateStopped` - 已停止
- `StateError` - 错误状态

### 生命周期钩子

```go
builder := ioc.NewContainerBuilder().
    AddBeforeInitHook(func(ctx context.Context, store *ioc.Store) error {
        // 初始化前执行
        return nil
    }).
    AddAfterInitHook(func(ctx context.Context, store *ioc.Store) error {
        // 初始化后执行
        return nil
    }).
    AddBeforeDestroyHook(func(ctx context.Context, store *ioc.Store) error {
        // 销毁前执行
        return nil
    }).
    AddAfterDestroyHook(func(ctx context.Context, store *ioc.Store) error {
        // 销毁后执行
        return nil
    })
```

## 高级功能

### Gin 路由注册

IoC 容器支持自动注册实现了 `GinRouter` 接口的组件到 Gin 路由器：

```go
// 创建 Gin 引擎
r := gin.Default()

// 注册实现了 GinRouter 接口的控制器组件
container.RegisterController(&UserController{name: "userController"})
container.RegisterController(&ProductController{name: "productController"})

// 初始化容器
ctx := context.Background()
err := container.Initialize(ctx)
if err != nil {
    log.Fatal(err)
}

// 自动注册所有 GinRouter 组件的路由
err = container.Store().GinRouterRegister(r)
if err != nil {
    log.Fatal("路由注册失败:", err)
}

// 启动服务器
r.Run(":8080")
```

**路由注册流程：**
1. 容器会遍历所有命名空间中的组件
2. 检查组件是否实现了 `GinRouter` 接口
3. 自动调用组件的 `RegisterGinRoutes` 方法注册路由
4. 按命名空间和组件顺序依次注册

### 健康检查

```go
// 执行健康检查
report := container.HealthCheck(ctx)
if report.Status == ioc.HealthStatusHealthy {
    log.Info("容器健康")
} else {
    log.Error("容器不健康: %s", report.Message)
}
```

### 容器度量

```go
// 获取容器度量信息
metrics := container.GetMetrics(ctx)
log.Info("已注册组件数: %d", metrics.RegisteredComponents)
log.Info("运行中组件数: %d", metrics.RunningComponents)
log.Info("上次初始化时间: %v", metrics.LastInitTime)
```

### 重启容器

```go
// 重启容器
err := container.Restart(ctx)
if err != nil {
    log.Error("容器重启失败: %v", err)
}
```

## 错误处理

模块定义了以下常见错误：

- `ErrNamespaceNotFound` - 命名空间未找到
- `ErrComponentNotFound` - 组件未找到
- `ErrComponentAlreadyExists` - 组件已存在
- `ErrNamespaceAlreadyExists` - 命名空间已存在
- `ErrInvalidComponent` - 无效组件
- `ErrDependencyNotFound` - 依赖未找到
- `ErrCircularDependency` - 循环依赖

## 最佳实践

1. **组件命名**：使用清晰、唯一的组件名称
2. **命名空间使用**：按功能层次组织组件
3. **依赖管理**：避免循环依赖，使用依赖注入接口
4. **错误处理**：正确处理初始化和销毁过程中的错误
5. **资源清理**：实现 Destroyer 接口进行资源清理
6. **健康检查**：为关键组件实现健康检查
7. **排序使用**：合理设置组件初始化顺序

## 示例应用

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/gin-gonic/gin"
    "github.com/kochabx/kit/ioc"
)

func main() {
    // 创建容器
    container := ioc.NewApplicationContainer(
        ioc.WithApplicationVerbose(),
        ioc.WithApplicationShutdownTimeout(30*time.Second),
    )
    
    // 注册组件
    container.RegisterConfig(&ConfigComponent{name: "appConfig"})
    container.RegisterDatabase(&DatabaseComponent{name: "mainDB"})
    container.RegisterBaseComponent(&CacheComponent{name: "redisCache"})
    container.RegisterHandler(&UserHandler{name: "userHandler"})
    container.RegisterController(&UserController{name: "userController"})
    container.RegisterController(&ProductController{name: "productController"})
    
    // 初始化容器
    ctx := context.Background()
    if err := container.Initialize(ctx); err != nil {
        log.Fatal("容器初始化失败:", err)
    }
    
    // 创建 Gin 引擎并注册路由
    r := gin.Default()
    if err := container.Store().GinRouterRegister(r); err != nil {
        log.Fatal("路由注册失败:", err)
    }
    
    // 启动 HTTP 服务器
    go func() {
        if err := r.Run(":8080"); err != nil {
            log.Error("HTTP 服务器启动失败:", err)
        }
    }()
    
    // 等待应用结束信号...
    
    // 优雅关闭
    if err := container.Shutdown(ctx); err != nil {
        log.Error("容器关闭失败:", err)
    }
}
```

## 最佳实践

1. **组件命名**：使用清晰、唯一的组件名称
2. **命名空间使用**：按功能层次组织组件
3. **依赖管理**：避免循环依赖，使用依赖注入接口
4. **错误处理**：正确处理初始化和销毁过程中的错误
5. **资源清理**：实现 Destroyer 接口进行资源清理
6. **健康检查**：为关键组件实现健康检查
7. **排序使用**：合理设置组件初始化顺序
8. **路由注册**：将控制器组件注册到 `controller` 命名空间，实现 `GinRouter` 接口
9. **路由分组**：在 `RegisterGinRoutes` 方法中使用路由分组组织 API

## 注意事项

- 组件名称在同一命名空间内必须唯一
- 初始化顺序由命名空间优先级和组件排序值决定
- 支持上下文取消和超时控制
- 线程安全，支持并发访问
- 建议在应用启动时完成所有组件注册
- `GinRouterRegister` 应该在容器初始化完成后调用
- 路由注册会按照命名空间优先级和组件顺序进行
