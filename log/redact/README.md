# redact

`redact` 为日志提供字段级和内容级脱敏。规则在创建时完成校验和编译，读取路径使用不可变快照；规则可以在运行时安全地增加、删除、启用或禁用。

## 快速开始

下面是一组适合多数服务的基础规则：

```go
package main

import (
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/log/redact"
)

func main() {
	r, err := redact.New(
		redact.Field("password", redact.Replace("******")),
		redact.Field("token", redact.Replace("******")),
		redact.Field("secret", redact.Replace("******")),
		redact.Field("phone", redact.KeepEdges(3, 4)),
		redact.Field("idcard", redact.KeepEdges(6, 4)),
		redact.Field("bankcard", redact.KeepEdges(4, 4)),
		redact.Content("phone-content", `1[3-9]\d{9}`, redact.KeepEdges(3, 4)),
		redact.Content(
			"email-content",
			`\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`,
			redact.Email(),
		),
	)
	if err != nil {
		panic(err)
	}

	logger := log.New(log.WithRedactor(r))
	logger.Info().
		Str("phone", "13812345678").
		Str("email", "alice@example.com").
		Msg("contact phone=13912345678")
}
```

输出中的敏感信息会被处理为类似内容：

```json
{"phone":"138****5678","email":"a***e@example.com","message":"contact phone=139****5678"}
```

## 规则类型

### 字段规则

`Field` 精确匹配 JSON 字段名，适合处理结构化日志字段：

```go
r, err := redact.New(
	redact.Field("password", redact.Replace("******")),
	redact.Field("mobile", redact.KeepEdges(3, 4)),
)
```

字段名区分大小写。`phone` 不会匹配 `Phone`、`user_phone` 或嵌入普通文本中的内容。

### 内容规则

`Content` 使用正则表达式匹配整条日志中的文本，适合处理消息、错误文本及未结构化内容：

```go
r, err := redact.New(
	redact.Content(
		"access-key",
		`AKIA[A-Z0-9]{16}`,
		redact.Replace("******"),
	),
)
```

内容规则会扫描日志文本，规则数量和正则复杂度会直接影响开销。能够使用结构化字段时，优先使用 `Field`。

## 脱敏策略

### 完整替换

```go
redact.Replace("******")
```

### 保留首尾

```go
redact.KeepEdges(3, 4) // 13812345678 -> 138****5678
```

当原始值过短、无法安全保留指定首尾长度时，会完整隐藏该值。

### 邮箱

```go
redact.Email() // alice@example.com -> a***e@example.com
```

邮箱策略保留域名以及本地部分的首尾字符。

## 直接处理字符串

非日志场景可以直接调用 `RedactString`：

```go
r, err := redact.New(
	redact.Field("token", redact.Replace("******")),
)
if err != nil {
	panic(err)
}

result := r.RedactString(`{"token":"abc123","name":"alice"}`)
// {"token":"******","name":"alice"}
```

## 包装 io.Writer

需要为已有输出流增加脱敏时，可以使用 `NewWriter`：

```go
var output bytes.Buffer

r, err := redact.New(
	redact.Content("phone", `1[3-9]\d{9}`, redact.KeepEdges(3, 4)),
)
if err != nil {
	panic(err)
}

w := redact.NewWriter(&output, r)
_, err = w.Write([]byte("phone=13812345678"))
if err != nil {
	panic(err)
}

// output.String() == "phone=138****5678"
```

## 动态管理规则

规则更新会作为一个完整快照原子发布，可以与日志写入并发执行：

```go
if err := r.AddRule(
	redact.Field("api_key", redact.Replace("******")),
); err != nil {
	return err
}

r.DisableRule("api_key")
r.EnableRule("api_key")

enabled := r.IsEnabled("api_key")
removed := r.RemoveRule("api_key")
```

规则名称必须唯一。新增规则名称重复、正则无效或脱敏策略为空时，`New` 和 `AddRule` 会返回错误。

## 建议

- 在日志产生处使用稳定、统一的字段名。
- 密码、令牌、密钥等凭证应使用 `Replace` 完整隐藏。
- 只有确有排障需求时，才使用 `KeepEdges` 保留部分标识。
- 内容规则应使用边界明确的正则，避免误匹配和不必要的扫描开销。
- 脱敏是日志输出的最后防线，不应替代“不记录敏感数据”的设计原则。
