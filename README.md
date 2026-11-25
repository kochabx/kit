# Kit

## 模块一览

### 核心包

- [`app/`](app/README.md) — 进程生命周期、`os.Signal` 监听与优雅停机
- [`cx/`](cx/README.md) — 类型化依赖容器、健康检查聚合与 `errgroup` 运行器监督
- [`fault/`](fault/README.md) — 不可变领域错误、HTTP/gRPC 状态码映射与 RFC 9457 `Problem`
- [`log/`](log/README.md) — 基于 `log/slog` 的控制台（`tint`）与轮转 JSON 文件（`timberjack`）日志，内置递归脱敏、上下文字段提取与统一时间格式
- [`config/`](config/README.md) — 基于 `spf13/viper` 的配置加载（默认 `json` tag）、原子快照读取与 `fsnotify` 热更新

### 扩展包

| 分类 | 包路径 | 说明 | 文档 |
|---|---|---|---|
| 默认值与校验 | `x/defaults` | 结构体 tag 默认值注入与原子回滚（纯标准库） | [defaults](x/defaults/README.md) |
| | `x/validate` | 基于 `validator/v10` 的 en/zh 校验与 `*fault.Error` 映射 | [validate](x/validate/README.md) |
| 密码学 | `x/crypto` | `hpke/`（含 `MLKEM768X25519+SHAKE256`）、`aead/`（每消息 HKDF）、`sign/`（HMAC / Ed25519 / `ML-DSA`） | [crypto](x/crypto/README.md) |
| 可观测 | `x/otel` | OTLP Trace/Metric SDK 生命周期管理与 `log.ContextExtractor` | [otel](x/otel/README.md) |
| 传输层 | `x/transport/http` | `net/http` Server、泛型 `Router.Handle[Req, Resp]`、`otelhttp`、CORS、CSRF、Scalar OpenAPI UI 与中间件 | [transport](x/transport/README.md) |
| | `x/transport/http/gin` | Gin `Adapt` 中间件桥接与 `WriteProblem` | [transport](x/transport/README.md) |
| | `x/transport/http/prom` | Prometheus HTTP 请求指标中间件与 `/metrics` Handler | [transport](x/transport/README.md) |
| | `x/transport/grpc` | gRPC Server、`go-grpc-middleware/v2`、`otelgrpc`、`grpc/health/v1` 与 reflection | [transport](x/transport/README.md) |
| 出站客户端 | `x/client/http` | `resty.dev/v3` + `otelhttp` + 泛型 `Get`/`Post`/`Put`/`Delete` | [client](x/client/README.md) |
| | `x/client/ws` | `coder/websocket` + `cenkalti/backoff/v5` + `iter.Seq2` 保序消费流 | [client](x/client/README.md) |
| 存储与消息 | `x/store/pg` | `jackc/pgx/v5/pgxpool` + `otelpgx` + `slog` 查询追踪 | [store](x/store/README.md) |
| | `x/store/gorm` | GORM 核心封装（`otelgorm`、参数脱敏 `slog` Logger 与连接池） | [store](x/store/README.md) |
| | `x/store/gorm/postgres` | PostgreSQL GORM 驱动封装（基于 `pgx`） | [store](x/store/README.md) |
| | `x/store/gorm/mysql` | MySQL GORM 驱动封装（TLS 克隆、Unix socket） | [store](x/store/README.md) |
| | `x/store/gorm/sqlite` | 纯 Go `modernc.org/sqlite` 驱动封装（支持 `CGO_ENABLED=0`） | [store](x/store/README.md) |
| | `x/store/redis` | `go-redis/v9`（Standalone/Sentinel/Cluster）+ `redisotel` + 慢查询 Hook | [store](x/store/README.md) |
| | `x/store/kafka` | `twmb/franz-go/pkg/kgo` + `kotel` + `kslog` + SCRAM/TLS | [store](x/store/README.md) |
| | `x/store/mongo` | `mongo-driver/v2` + `otelmongo`（URI / 副本集 / TLS） | [store](x/store/README.md) |
| | `x/store/etcd` | `etcd/client/v3` 多节点探活、分布式锁、服务注册与配置读取 | [store](x/store/README.md) |
| 认证与限流 | `x/auth/jwt` | `lestrrat-go/jwx/v3` + `uuid.NewV7()` + 密钥轮换与 JWKS | [auth](x/auth/README.md) |
| | `x/auth/jwt/redis` | 基于 `redis.UniversalClient` 与 `EVALSHA` 的会话存储 | [auth](x/auth/README.md) |
| | `x/auth/mfa` | `pquerna/otp` TOTP/HOTP 验证与严格 TTL 防重放 | [auth](x/auth/README.md) |
| | `x/auth/mfa/qr` | `go-qrcode/v2` PNG/SVG 二维码生成 | [auth](x/auth/README.md) |
| | `x/rate` | `redis_rate/v10`（O(1) GCRA）+ 本地限流 | [rate](x/rate/README.md) |
| 异步任务 | `x/asynq` | `github.com/hibiken/asynq` 客户端、Worker 与调度器生命周期封装 | [asynq](x/asynq/README.md) |

## 快速开始

```bash
go get github.com/kochabx/kit
```

```go
package main

import (
	"context"
	"net/http"

	"github.com/kochabx/kit/app"
	kithttp "github.com/kochabx/kit/x/transport/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"pong"}`))
	})

	application, err := app.New(
		app.WithServers(kithttp.NewServer(mux, kithttp.WithAddr(":8080"))),
	)
	if err != nil {
		panic(err)
	}
	if err := application.Run(context.Background()); err != nil {
		panic(err)
	}
}
```

## 常用命令

```bash
make tidy       # 整理 go.mod 与 go.sum
make fmt-check  # 检查 gofmt 格式
make build      # CGO_ENABLED=0 全量编译
make vet        # 全量 go vet 静态检查
make fix-check  # 全量 go fix -diff 检查
make test       # CGO_ENABLED=0 单元测试 + CGO_ENABLED=1 -race 竞态测试
make check      # 顺序执行 fmt-check build vet fix-check test
```
