# fault

不可变结构化领域错误库，提供标准错误码、HTTP 状态码与 gRPC 状态码自动映射，以及 RFC 9457 `Problem` 响应体转换。

## 快速开始

```go
import (
	"errors"

	"github.com/kochabx/kit/fault"
)

func getUser(id string) error {
	if id == "" {
		return fault.InvalidArgument("user id is required")
	}
	return fault.NotFound("user not found").
		With("user_id", id).
		WithCause(errors.New("sql: no rows"))
}
```

`*fault.Error` 是不可变对象；`With`、`WithMetadata`、`WithCause`、`WithStatus` 均返回新副本，不会修改原实例。

## 内置构造器与状态映射

| 构造器 | `fault.Code` | 默认 HTTP 状态码 | 默认 gRPC Code |
|---|---|---|---|
| `InvalidArgument(msg)` | `INVALID_ARGUMENT` | `400` | `InvalidArgument` |
| `BadRequest(msg)` | `BAD_REQUEST` | `400` | `InvalidArgument` |
| `Unauthorized(msg)` | `UNAUTHORIZED` | `401` | `Unauthenticated` |
| `Forbidden(msg)` | `FORBIDDEN` | `403` | `PermissionDenied` |
| `NotFound(msg)` | `NOT_FOUND` | `404` | `NotFound` |
| `Conflict(msg)` | `CONFLICT` | `409` | `AlreadyExists` |
| `UnprocessableEntity(msg)` | `UNPROCESSABLE` | `422` | `InvalidArgument` |
| `TooManyRequests(msg)` | `RATE_LIMITED` | `429` | `ResourceExhausted` |
| `Canceled(msg)` | `CANCELED` | `499` | `Canceled` |
| `Internal(msg)` | `INTERNAL` | `500` | `Internal` |
| `NotImplemented(msg)` | `NOT_IMPLEMENTED` | `501` | `Unimplemented` |
| `BadGateway(msg)` | `BAD_GATEWAY` | `502` | `Unavailable` |
| `ServiceUnavailable(msg)` | `UNAVAILABLE` | `503` | `Unavailable` |
| `DeadlineExceeded(msg)` | `DEADLINE_EXCEEDED` | `504` | `DeadlineExceeded` |
| `GatewayTimeout(msg)` | `GATEWAY_TIMEOUT` | `504` | `DeadlineExceeded` |

自定义错误码可使用 `fault.New(code, msg)` / `fault.Newf(code, format, args...)`，并通过 `WithStatus(httpStatus)` 覆盖 HTTP 状态码。

## 错误提取与 RFC 9457 Problem

- `fault.From(err)`：使用 `errors.AsType[*fault.Error]` 提取领域错误；
- `fault.CodeOf(err)` / `fault.HTTPStatusOf(err)`：自动兼容包装链中的 `*fault.Error`、`context.Canceled` 与 `context.DeadlineExceeded`；
- `err.GRPCCode()` / `status.Error` 配合传输层映射；
- `err.Problem(instance)`：生成脱敏后的 RFC 9457 `Problem`（故意不输出底层 `cause`，避免泄露内部实现细节）。

## 验证

```bash
go test -race -count=1 ./fault/...
```
