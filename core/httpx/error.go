package httpx

import (
	"fmt"
	"net/http"
)

// HTTPError 表示一次 HTTP 请求虽然在网络层完成，但服务端返回了被判定为错误的状态码。
//
// 是否将某个状态码视为错误由 Client 的 WithErrorOnStatus 选项决定，默认 code >= 400。
type HTTPError struct {
	Method     string      // 请求方法
	URL        string      // 请求最终 URL
	StatusCode int         // HTTP 状态码
	Status     string      // 状态行 (例: "404 Not Found")
	Header     http.Header // 响应头
	Body       []byte      // 响应体快照 (最多 ErrorBodyLimit 字节)
}

// ErrorBodyLimit 出现 HTTPError 时，最多保留多少字节的响应体用于诊断。
const ErrorBodyLimit = 4 * 1024

// Error 实现 error 接口。
func (e *HTTPError) Error() string {
	if len(e.Body) == 0 {
		return fmt.Sprintf("httpx: %s %s -> %s", e.Method, e.URL, e.Status)
	}
	return fmt.Sprintf("httpx: %s %s -> %s: %s", e.Method, e.URL, e.Status, e.Body)
}

// Is 允许通过 errors.Is 按 StatusCode 匹配。
//
//	errors.Is(err, &HTTPError{StatusCode: 404})
func (e *HTTPError) Is(target error) bool {
	t, ok := target.(*HTTPError)
	if !ok {
		return false
	}
	return t.StatusCode == e.StatusCode
}
