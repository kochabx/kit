package response

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/errors"
)

const (
	// 成功响应常量
	defaultSuccessMessage = "success"
	successCode           = http.StatusOK

	// 错误响应常量
	defaultErrorMessage = "service temporarily unavailable"
	defaultErrorCode    = http.StatusServiceUnavailable
)

type Response struct {
	Code    int    `json:"code"`              // 业务逻辑代码
	Data    any    `json:"data,omitempty"`    // 响应数据，为nil时省略
	Message string `json:"message,omitempty"` // 响应消息，为空时省略
}

// reset 清空所有字段用于对象池复用，防止内存泄漏
func (r *Response) reset() {
	r.Code = 0
	r.Data = nil
	r.Message = ""
}

// setSuccess 配置成功响应
func (r *Response) setSuccess(data any) {
	r.Code = successCode
	r.Data = data
	r.Message = defaultSuccessMessage
}

// setError 配置错误响应
func (r *Response) setError(code int, message string) {
	r.Code = code
	r.Data = nil
	r.Message = message
}

// 对象池用于复用Response实例
var responsePool = sync.Pool{
	New: func() any {
		return &Response{}
	},
}

// acquireResponse 从对象池中获取Response实例以提升性能
func acquireResponse() *Response {
	return responsePool.Get().(*Response)
}

// releaseResponse 重置并将Response实例返回到对象池
func releaseResponse(r *Response) {
	if r != nil {
		r.reset()
		responsePool.Put(r)
	}
}

func NewResponse(code int, data any, message string) *Response {
	return &Response{
		Code:    code,
		Data:    data,
		Message: message,
	}
}

// GinJSON 使用响应对象池写入成功的JSON响应
func GinJSON(c *gin.Context, data any) {
	if c == nil {
		return
	}

	resp := acquireResponse()
	defer releaseResponse(resp)

	resp.setSuccess(data)
	c.JSON(successCode, resp)
}

// GinJSONE 使用响应对象池写入错误JSON响应
func GinJSONE(c *gin.Context, err error) {
	if c == nil {
		return
	}

	defer c.Abort()

	resp := acquireResponse()
	defer releaseResponse(resp)

	if err == nil {
		resp.setError(defaultErrorCode, defaultErrorMessage)
		c.JSON(http.StatusOK, resp)
		return
	}

	e := errors.FromError(err)

	resp.setError(e.Code, e.Message)
	c.JSON(http.StatusOK, resp)
}

// 流式接口用于构建响应对象

// Builder 提供流式接口用于构建Response对象的方法链
type Builder struct {
	response *Response
	usePool  bool
}

// NewBuilder 创建新的响应构建器，可选择使用对象池
func NewBuilder(usePool bool) *Builder {
	var resp *Response
	if usePool {
		resp = acquireResponse()
	} else {
		resp = &Response{}
	}

	return &Builder{
		response: resp,
		usePool:  usePool,
	}
}

// WithCode 设置响应代码并返回构建器以支持方法链
func (b *Builder) WithCode(code int) *Builder {
	b.response.Code = code
	return b
}

// WithData 设置响应数据并返回构建器以支持方法链
func (b *Builder) WithData(data any) *Builder {
	b.response.Data = data
	return b
}

// WithMessage 设置响应消息并返回构建器以支持方法链
func (b *Builder) WithMessage(message string) *Builder {
	b.response.Message = message
	return b
}

// Build 返回构建的Response，可选择将其释放回池中
func (b *Builder) Build() *Response {
	result := b.response
	if b.usePool {
		// 如果使用对象池，创建一个副本以避免修改池中的实例
		copy := &Response{
			Code:    result.Code,
			Data:    result.Data,
			Message: result.Message,
		}
		releaseResponse(b.response)
		return copy
	}
	return result
}

// GinJSONWithBuilder 使用构建器模式写入复杂的JSON响应
func GinJSONWithBuilder(c *gin.Context, builderFunc func(*Builder)) {
	if c == nil {
		return
	}

	builder := NewBuilder(true)
	builderFunc(builder)
	resp := builder.Build()

	c.JSON(http.StatusOK, resp)
}
