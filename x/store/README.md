# x/store

基础设施存储与消息客户端合集，均提供结构化 `Config` 校验、延迟建连、`Start` / `HealthCheck` / `Stop` 生命周期管理，以及 `slog` 日志与 OpenTelemetry 链路追踪：

- `github.com/kochabx/kit/x/store/pg` — 原生 `jackc/pgx/v5/pgxpool` 连接池（零 ORM 依赖，直接兼容 `sqlc`）
- `github.com/kochabx/kit/x/store/gorm` + `{postgres,mysql,sqlite}` — GORM 公共壳与三种方言驱动（SQLite 采用纯 Go `modernc.org/sqlite`，零 CGO）
- `github.com/kochabx/kit/x/store/redis` — `go-redis/v9` 统一客户端（Standalone / Sentinel / Cluster）+ 慢查询 Hook
- `github.com/kochabx/kit/x/store/mongo` — `mongo-driver/v2`（支持 URI、多节点主机、副本集与无认证开发环境）
- `github.com/kochabx/kit/x/store/kafka` — `twmb/franz-go/pkg/kgo` + `kotel` + `kslog` + SCRAM/TLS
- `github.com/kochabx/kit/x/store/etcd` — `etcd/client/v3` + 多端点健康探活 + `Locker` 分布式锁 + `Registry` 服务注册 + `ConfigProvider`

---

## 1. PostgreSQL（原生 `pgxpool` 与 `sqlc`）

```go
pgClient, err := pg.New(pg.Config{
	Host:     "127.0.0.1",
	Port:     5432,
	Database: "app",
	Username: "postgres",
	Password: secret,
}, pg.WithLogger(logger.Slog()))
if err != nil {
	return err
}
defer pgClient.Close()

// 直接将 *pgxpool.Pool 传给 sqlc 生成的 Queries
queries := db.New(pgClient.Pool())
```

---

## 2. GORM（PostgreSQL / MySQL / SQLite）

```go
dbClient, err := postgres.New(postgres.Config{
	Host:     "127.0.0.1",
	Database: "app",
	Username: "postgres",
	Password: secret,
	Gorm: kitgorm.Config{
		SlowThreshold: 200 * time.Millisecond,
	},
}, kitgorm.WithLogger(logger.Slog()))
// MySQL: mysql.New(mysql.Config{...})
// SQLite (CGO_ENABLED=0): sqlite.New(sqlite.Config{Path: "app.db"})
```

通过 `dbClient.DB()` 获取 `*gorm.DB`，`dbClient.SQLDB()` 获取底层 `*sql.DB`。

---

## 3. Redis / MongoDB / Kafka / etcd

- **Redis（单机 / 哨兵 / 集群）**：通过 `Addrs`、`MasterName`、`Cluster` 自动切换 `redis.UniversalClient` 模式，内置 `redisotel` 与慢查询日志 Hook；通过 `rdb.Client()` 获取原生 `goredis.UniversalClient`。
- **MongoDB**：支持 `URI` 或 `Hosts` + `ReplicaSet`；未填 `Username` 时不强制开启认证；通过 `mc.Client()` / `mc.Database()` 操作集合。
- **Kafka**：支持多 `Brokers`、SCRAM-SHA-256 / SHA-512、TLS 与 OTel 链路注入；通过 `kc.Client()` 获取 `*kgo.Client`，停机自动 `Flush` + `LeaveGroupContext`。
- **etcd**：内置 `NewLocker(client, prefix, ttlSeconds)`、`NewRegistry(client, target)` 与 `NewConfigProvider(client, key)`。

## 验证

```bash
go test -race -count=1 ./x/store/...
```
