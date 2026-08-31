# Database

`store/db` 是基于 GORM 的数据库客户端，支持 MySQL、PostgreSQL 和 SQLite。它负责结构化连接配置、DSN 构建、连接池配置、连接检查以及数据库生命周期管理。

## 快速开始

```go
package main

import (
	"log"

	"github.com/kochabx/kit/store/db"
)

func main() {
	client, err := db.New(db.Config{
		Driver: db.PostgresConfig{
			Host:     "127.0.0.1",
			Database: "app",
			Password: "secret",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	gormDB := client.DB()
	_ = gormDB
}
```

`New` 会应用默认值、校验配置、构造 GORM dialector、打开数据库并配置连接池。GORM 默认会在打开数据库时执行一次 Ping，初始化失败时不会返回部分可用的 Client。

## Driver 配置

调用方通过 `Config.Driver` 选择具体数据库。模块根据结构化配置安全构建 DSN，不需要调用方自行拼接。

### MySQL

```go
client, err := db.New(db.Config{
	Driver: db.MySQLConfig{
		Host:         "mysql.internal",
		Port:         3306,
		User:         "app",
		Password:     "secret",
		Database:     "app",
		ParseTime:    true,
		Location:     "UTC",
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	},
})
```

默认值：

| 字段 | 默认值 |
| --- | --- |
| `Host` | `127.0.0.1` |
| `Port` | `3306` |
| `User` | `root` |
| `Network` | `tcp` |
| `Charset` | `utf8mb4` |
| `Location` | `Local` |
| `Timeout` | `10s` |

`Database` 必填。密码不会出现在模块返回的错误上下文中。

### PostgreSQL

```go
client, err := db.New(db.Config{
	Driver: db.PostgresConfig{
		Host:           "postgres.internal",
		Port:           5432,
		User:           "app",
		Password:       "secret",
		Database:       "app",
		SSLMode:        "require",
		TimeZone:       "UTC",
		ConnectTimeout: 5 * time.Second,
	},
})
```

默认值：

| 字段 | 默认值 |
| --- | --- |
| `Host` | `127.0.0.1` |
| `Port` | `5432` |
| `User` | `postgres` |
| `SSLMode` | `disable` |
| `TimeZone` | `Asia/Shanghai` |
| `ConnectTimeout` | `10s` |

`Database` 必填。

### SQLite

```go
client, err := db.New(db.Config{
	Driver: db.SQLiteConfig{
		Path:        "data/app.db",
		ForeignKeys: true,
	},
})
```

内存数据库：

```go
client, err := db.New(db.Config{
	Driver: db.SQLiteConfig{Path: ":memory:"},
})
```

默认值：

| 字段 | 默认值 |
| --- | --- |
| `JournalMode` | `WAL` |
| `CacheSize` | `-2000` |
| `BusyTimeout` | `5s` |
| `SyncMode` | `NORMAL` |

`Path` 必填。SQLite 默认最多打开一个连接，避免多个连接访问内存数据库或产生不符合预期的并发行为。

## 连接池

不提供 `Pool` 时使用模块默认值：

| Driver | MaxIdleConns | MaxOpenConns | ConnMaxLifetime | ConnMaxIdleTime |
| --- | ---: | ---: | --- | --- |
| MySQL / PostgreSQL | 10 | 100 | 1h | 10m |
| SQLite | 1 | 1 | 0 | 0 |

自定义连接池：

```go
client, err := db.New(db.Config{
	Driver: db.PostgresConfig{Database: "app"},
	Pool: &db.PoolConfig{
		MaxIdleConns:    20,
		MaxOpenConns:    200,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 10 * time.Minute,
	},
})
```

`Pool == nil` 表示使用模块默认值；非 nil 的 `PoolConfig` 会原样应用，因此 `&db.PoolConfig{}` 表示显式使用 `database/sql` 的零值行为。所有数值必须非负，且在 `MaxOpenConns > 0` 时，`MaxIdleConns` 不能大于 `MaxOpenConns`。

## GORM 日志

默认不记录 GORM 日志。只有传入 `WithLogger` 才会把 GORM 日志写入项目 logger：

```go
import (
	"gorm.io/gorm/logger"

	kitlog "github.com/kochabx/kit/log"
)

client, err := db.New(db.Config{
	Driver:             db.PostgresConfig{Database: "app"},
	LogLevel:           logger.Warn,
	SlowQueryThreshold: 200 * time.Millisecond,
}, db.WithLogger(kitlog.Global()))
```

传入 `WithLogger` 但未设置 `LogLevel` 时，默认使用 `logger.Info`。可用级别为 `logger.Silent`、`logger.Error`、`logger.Warn` 和 `logger.Info`。

如果 `GORMConfig.Logger` 已设置，它的优先级高于 `WithLogger`、`LogLevel` 和 `SlowQueryThreshold`。

## GORM 高级配置

可以传入原生 `gorm.Config`。模块会复制配置，不会修改调用方提供的值：

```go
client, err := db.New(db.Config{
	Driver: db.SQLiteConfig{Path: "app.db"},
	GORMConfig: &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:             true,
	},
})
```

模块保留 GORM 的 Automatic Ping。若在 `GORMConfig` 中设置 `DisableAutomaticPing: true`，则由调用方负责在合适的时机检查连接。

## GORM 插件

```go
client, err := db.New(cfg,
	db.WithPlugins(pluginA, pluginB),
)
```

插件会在初次 Ping 前安装。nil Option 或 nil 插件会返回 `db.ErrInvalidOption`。

## Client API

```go
gormDB := client.DB()
stats := client.Stats()

if err := client.Ping(ctx); err != nil {
	return err
}
if err := client.Close(); err != nil {
	return err
}
```

`Client` 同时实现项目组件生命周期所需的方法：

- `Start(ctx)`：执行 Ping
- `HealthCheck(ctx)`：执行 Ping
- `Stop(ctx)`：关闭连接池

使用应用组件管理数据库生命周期时，不要再对同一个 Client 执行 `defer client.Close()`，避免重复管理资源。

底层 `*sql.DB` 不作为 Client API 直接暴露。确有需要时，可以使用：

```go
sqlDB, err := client.DB().DB()
```

## Context 和错误处理

`New` 使用 GORM 的 Automatic Ping，因此不接收 Context。运行期连接检查使用 `Ping(ctx)`，调用方可以为健康检查设置超时。

配置和 Option 错误支持 `errors.Is`：

```go
client, err := db.New(cfg)
switch {
case errors.Is(err, db.ErrInvalidConfig):
	// 配置无效
case errors.Is(err, db.ErrInvalidOption):
	// Option 无效
case err != nil:
	// 打开或连接数据库失败
}
```

模块使用 `%w` 保留底层错误链，同时不会在错误上下文中输出密码或完整 DSN。

## JSON 配置说明

配置字段只提供 JSON tag。`Config.Driver` 是接口并标记为 `json:"-"`，调用方应先确定数据库类型，再把对应配置赋给 `Driver`；模块不会根据 JSON 中的字符串动态创建 driver。

```go
cfg := db.Config{
	Driver: db.MySQLConfig{
		Host:     "127.0.0.1",
		Database: "app",
	},
}
```

`logger.LogLevel` 在 JSON 中使用 GORM 的数字值：

| 值 | 级别 |
| ---: | --- |
| 1 | Silent |
| 2 | Error |
| 3 | Warn |
| 4 | Info |
