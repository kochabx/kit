package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/errors"
)

const (
	// HTTP 状态码常量 (从 net/http 导出以便使用)
	// 2xx Success
	StatusOK             = http.StatusOK             // 200
	StatusCreated        = http.StatusCreated        // 201
	StatusAccepted       = http.StatusAccepted       // 202
	StatusNoContent      = http.StatusNoContent      // 204
	StatusResetContent   = http.StatusResetContent   // 205
	StatusPartialContent = http.StatusPartialContent // 206

	// 3xx Redirection
	StatusMovedPermanently  = http.StatusMovedPermanently  // 301
	StatusFound             = http.StatusFound             // 302
	StatusSeeOther          = http.StatusSeeOther          // 303
	StatusNotModified       = http.StatusNotModified       // 304
	StatusTemporaryRedirect = http.StatusTemporaryRedirect // 307
	StatusPermanentRedirect = http.StatusPermanentRedirect // 308

	// 4xx Client Errors
	StatusBadRequest                   = http.StatusBadRequest                   // 400
	StatusUnauthorized                 = http.StatusUnauthorized                 // 401
	StatusPaymentRequired              = http.StatusPaymentRequired              // 402
	StatusForbidden                    = http.StatusForbidden                    // 403
	StatusNotFound                     = http.StatusNotFound                     // 404
	StatusMethodNotAllowed             = http.StatusMethodNotAllowed             // 405
	StatusNotAcceptable                = http.StatusNotAcceptable                // 406
	StatusProxyAuthRequired            = http.StatusProxyAuthRequired            // 407
	StatusRequestTimeout               = http.StatusRequestTimeout               // 408
	StatusConflict                     = http.StatusConflict                     // 409
	StatusGone                         = http.StatusGone                         // 410
	StatusLengthRequired               = http.StatusLengthRequired               // 411
	StatusPreconditionFailed           = http.StatusPreconditionFailed           // 412
	StatusRequestEntityTooLarge        = http.StatusRequestEntityTooLarge        // 413
	StatusRequestURITooLong            = http.StatusRequestURITooLong            // 414
	StatusUnsupportedMediaType         = http.StatusUnsupportedMediaType         // 415
	StatusRequestedRangeNotSatisfiable = http.StatusRequestedRangeNotSatisfiable // 416
	StatusExpectationFailed            = http.StatusExpectationFailed            // 417
	StatusTeapot                       = http.StatusTeapot                       // 418
	StatusMisdirectedRequest           = http.StatusMisdirectedRequest           // 421
	StatusUnprocessableEntity          = http.StatusUnprocessableEntity          // 422
	StatusLocked                       = http.StatusLocked                       // 423
	StatusFailedDependency             = http.StatusFailedDependency             // 424
	StatusTooEarly                     = http.StatusTooEarly                     // 425
	StatusUpgradeRequired              = http.StatusUpgradeRequired              // 426
	StatusPreconditionRequired         = http.StatusPreconditionRequired         // 428
	StatusTooManyRequests              = http.StatusTooManyRequests              // 429
	StatusRequestHeaderFieldsTooLarge  = http.StatusRequestHeaderFieldsTooLarge  // 431
	StatusUnavailableForLegalReasons   = http.StatusUnavailableForLegalReasons   // 451

	// 5xx Server Errors
	StatusInternalServerError           = http.StatusInternalServerError           // 500
	StatusNotImplemented                = http.StatusNotImplemented                // 501
	StatusBadGateway                    = http.StatusBadGateway                    // 502
	StatusServiceUnavailable            = http.StatusServiceUnavailable            // 503
	StatusGatewayTimeout                = http.StatusGatewayTimeout                // 504
	StatusHTTPVersionNotSupported       = http.StatusHTTPVersionNotSupported       // 505
	StatusVariantAlsoNegotiates         = http.StatusVariantAlsoNegotiates         // 506
	StatusInsufficientStorage           = http.StatusInsufficientStorage           // 507
	StatusLoopDetected                  = http.StatusLoopDetected                  // 508
	StatusNotExtended                   = http.StatusNotExtended                   // 510
	StatusNetworkAuthenticationRequired = http.StatusNetworkAuthenticationRequired // 511
)

const (
	// 默认响应消息
	defaultSuccessMsg = "success"
	defaultErrorMsg   = "operation failed"

	// 默认响应码
	successCode = http.StatusOK
)

// Response 表示标准化的 API 响应结构
// 使用泛型 T 来支持任意类型的数据字段
type Response[T any] struct {
	Code int    `json:"code"`           // 业务状态码
	Msg  string `json:"msg,omitempty"`  // 响应消息
	Data T      `json:"data,omitempty"` // 响应数据
}

// GinJSON 写入成功的 JSON 响应
// HTTP 状态码固定为 200，业务码为 200，消息为 "success"
//
// 示例：
//
//	user := User{ID: 1, Name: "Alice"}
//	GinJSON(c, user)
//	// 输出: {"code":200, "msg":"success", "data":{"id":1,"name":"Alice"}}
func GinJSON(c *gin.Context, data any) {
	if c == nil {
		return
	}

	c.JSON(http.StatusOK, &Response[any]{
		Code: successCode,
		Msg:  defaultSuccessMsg,
		Data: data,
	})
}

// GinJSONE 写入带有自定义业务码的 JSON 响应
// HTTP 状态码固定为 200，业务码和消息根据参数决定
//
// data 参数支持多种类型：
//   - error: 自动提取错误消息，优先从 kit/errors.Error 中提取
//   - string: 直接作为消息使用
//   - nil: 使用默认错误消息
//   - 其他类型: 作为 data 字段返回，消息为空
//
// 示例：
//
//	// 1. 传入 error
//	err := errors.New(10001, "用户名已存在")
//	GinJSONE(c, 10001, err)
//	// 输出: {"code":10001, "msg":"用户名已存在"}
//
//	// 2. 传入 string
//	GinJSONE(c, 400, "invalid parameters")
//	// 输出: {"code":400, "msg":"invalid parameters"}
//
//	// 3. 传入 nil
//	GinJSONE(c, 500, nil)
//	// 输出: {"code":500, "msg":"operation failed"}
//
//	// 4. 传入数据对象
//	GinJSONE(c, 201, map[string]any{"id": 123})
//	// 输出: {"code":201, "data":{"id":123}}
func GinJSONE(c *gin.Context, code int, data any) {
	if c == nil {
		return
	}

	var msg string
	var respData any

	switch v := data.(type) {
	case error:
		// 从 error 中提取消息
		msg = extractErrorMessage(v)
	case string:
		// 直接使用字符串作为消息
		msg = v
	case nil:
		// 使用默认错误消息
		msg = defaultErrorMsg
	default:
		// 其他类型作为 data 返回
		respData = v
	}

	c.JSON(http.StatusOK, &Response[any]{
		Code: code,
		Msg:  msg,
		Data: respData,
	})
}

// extractErrorMessage 从 error 中提取消息
// 优先尝试从 kit/errors.Error 中提取，如果失败则使用 Error() 方法
func extractErrorMessage(err error) string {
	if err == nil {
		return defaultErrorMsg
	}

	// 尝试转换为 kit/errors.Error 以获取结构化错误信息
	if e := errors.FromError(err); e != nil {
		return e.Message
	}

	// 降级使用标准 Error() 方法
	return err.Error()
}

// Success 创建成功响应对象（辅助函数）
// 适用于需要手动构造响应的场景
//
// 示例：
//
//	resp := Success(user)
//	c.JSON(http.StatusOK, resp)
func Success[T any](data T) *Response[T] {
	return &Response[T]{
		Code: successCode,
		Msg:  defaultSuccessMsg,
		Data: data,
	}
}

// Failure 创建失败响应对象（辅助函数）
// 适用于需要手动构造响应的场景
//
// 示例：
//
//	resp := Failure(404, "user not found")
//	c.JSON(http.StatusOK, resp)
func Failure(code int, msg string) *Response[any] {
	return &Response[any]{
		Code: code,
		Msg:  msg,
	}
}
