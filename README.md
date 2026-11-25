# Kit

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
