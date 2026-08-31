# MongoDB

`store/mongo` 是基于 MongoDB Go Driver v2 的轻量客户端，负责结构化配置、默认值与校验、连接池初始化、连接检查和应用生命周期管理。

## 快速开始

```go
package main

import (
	"context"
	"log"
	"time"

	kitmongo "github.com/kochabx/kit/store/mongo"
)

func main() {
	cfg := &kitmongo.Config{
		Hosts:      []string{"localhost:27017"},
		Username:   "app",
		Password:   "secret",
		AuthSource: "admin",
	}

	client, err := kitmongo.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		log.Fatal(err)
	}

	database := client.Database("app")
	_ = database
}
```

`New` 创建惰性 MongoDB Client，不会验证部署是否可达。需要在启动时确认连接时，应显式调用 `Ping`，或者把 Client 注册为应用组件，由 `Start` 执行检查。

## 配置

```go
cfg := &mongo.Config{
	Hosts:      []string{"mongo-1:27017", "mongo-2:27017"},
	Username:   "app",
	Password:   "secret",
	AuthSource: "admin",
	ReplicaSet: "rs0",

	MaxPoolSize: 100,
	MinPoolSize: 10,

	ConnectTimeout:         3 * time.Second,
	ServerSelectionTimeout: 5 * time.Second,
}
```

默认值：

| 字段 | 默认值 |
| --- | --- |
| `Hosts` | `localhost:27017` |
| `MaxPoolSize` | `10` |
| `ConnectTimeout` | `3s` |
| `ServerSelectionTimeout` | `3s` |

`New` 会复制配置后再应用默认值，不会修改调用方传入的 `Config` 或 `Hosts`。

### 认证

认证通过 MongoDB Driver 的 `options.Credential` 配置，不会手工拼接 URI。因此用户名和密码中的 `@`、`:`、`/` 等特殊字符不需要调用方自行转义。

没有设置 `Username`、`Password` 和 `AuthSource` 时不会启用认证。

### Replica Set

```go
cfg.Hosts = []string{
	"mongo-1:27017",
	"mongo-2:27017",
	"mongo-3:27017",
}
cfg.ReplicaSet = "rs0"
```

### Direct Connection

```go
cfg.Hosts = []string{"mongo.internal:27017"}
cfg.Direct = true
```

Direct connection 必须且只能配置一个 Host。

## Client API

获取数据库：

```go
database := client.Database("app")
collection := database.Collection("users")
```

执行健康检查：

```go
if err := client.Ping(ctx); err != nil {
	return err
}
```

关闭客户端：

```go
if err := client.Close(); err != nil {
	return err
}
```

通常不需要直接暴露底层 `*mongo.Client`。需要使用 Session、Transaction 等 Client API 时，可以通过 Database handle 获取：

```go
mongoClient := client.Database("app").Client()
```

## 应用生命周期

Client 实现：

- `Start(ctx)`：Ping primary
- `HealthCheck(ctx)`：Ping primary
- `Stop(ctx)`：使用关闭上下文断开连接

如果 Client 由应用组件管理，不要再对同一个实例执行 `defer client.Close()`。

## 错误处理

```go
client, err := mongo.New(cfg)
if errors.Is(err, mongo.ErrInvalidConfig) {
	// 配置无效
}
```

MongoDB Driver 返回的错误通过 `%w` 保留，可以继续使用 `errors.Is` 和 `errors.As` 检查底层错误。错误信息不会包含密码或手工构造的连接 URI。

## JSON

所有配置字段都提供 JSON tag。时间字段使用 `time.Duration`，通过 Go 标准 JSON 解码时对应纳秒整数；如需使用 `"3s"` 形式，应由上层配置系统提供 duration 解码能力。
