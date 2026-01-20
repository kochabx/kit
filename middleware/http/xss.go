package middleware

import (
	"bytes"
	"encoding/json"
	"html"
	"io"

	"github.com/gin-gonic/gin"
)

// Sanitizer XSS 过滤器接口
type Sanitizer interface {
	Sanitize(input string) string
}

// SanitizerFunc 过滤器函数适配器
type SanitizerFunc func(input string) string

func (f SanitizerFunc) Sanitize(input string) string {
	return f(input)
}

// HTMLEscapeSanitizer 创建 HTML 转义过滤器
func HTMLEscapeSanitizer() Sanitizer {
	return SanitizerFunc(html.EscapeString)
}

// XssConfig XSS 中间件配置
type XssConfig struct {
	QueryEnabled  bool                    // 过滤 Query 参数
	FormEnabled   bool                    // 过滤表单数据
	HeaderEnabled bool                    // 过滤请求头（慎用）
	BodyEnabled   bool                    // 过滤 JSON Body
	Sanitizer     Sanitizer               // 自定义过滤器
	SkipPaths     []string                // 跳过处理的路径前缀
	SkipFunc      func(*gin.Context) bool // 动态跳过判断函数
	SkipHeaders   []string                // 跳过过滤的请求头
}

// DefaultXssConfig 返回默认 XSS 配置
func DefaultXssConfig() XssConfig {
	return XssConfig{
		QueryEnabled:  true,
		FormEnabled:   true,
		HeaderEnabled: false, // 默认不过滤请求头，避免破坏正常功能
		BodyEnabled:   true,
		Sanitizer:     HTMLEscapeSanitizer(),
		SkipHeaders:   []string{"Authorization", "Content-Type", "Accept", "User-Agent"},
	}
}

// Xss 创建 XSS 防护中间件
func Xss(cfgs ...XssConfig) gin.HandlerFunc {
	cfg := DefaultXssConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Sanitizer == nil {
		cfg.Sanitizer = HTMLEscapeSanitizer()
	}

	// 构建跳过头部的 map
	skipHeaderMap := make(map[string]bool)
	for _, h := range cfg.SkipHeaders {
		skipHeaderMap[h] = true
	}

	// 预编译路径匹配器
	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(c *gin.Context) {
		// 检查是否跳过
		if shouldSkip(c, matcher, cfg.SkipFunc) {
			c.Next()
			return
		}

		// 过滤 Query 参数
		if cfg.QueryEnabled {
			sanitizeQuery(c, cfg.Sanitizer)
		}

		// 过滤表单数据
		if cfg.FormEnabled {
			sanitizeForm(c, cfg.Sanitizer)
		}

		// 过滤请求头
		if cfg.HeaderEnabled {
			sanitizeHeaders(c, cfg.Sanitizer, skipHeaderMap)
		}

		// 过滤 JSON Body
		if cfg.BodyEnabled {
			sanitizeJSONBody(c, cfg.Sanitizer)
		}

		c.Next()
	}
}

// sanitizeQuery 过滤 Query 参数
func sanitizeQuery(c *gin.Context, sanitizer Sanitizer) {
	query := c.Request.URL.Query()
	modified := false

	for key, values := range query {
		for i, value := range values {
			sanitized := sanitizer.Sanitize(value)
			if sanitized != value {
				query[key][i] = sanitized
				modified = true
			}
		}
	}

	if modified {
		c.Request.URL.RawQuery = query.Encode()
	}
}

// sanitizeForm 过滤表单数据
func sanitizeForm(c *gin.Context, sanitizer Sanitizer) {
	if err := c.Request.ParseForm(); err != nil {
		return
	}

	for key, values := range c.Request.PostForm {
		for i, value := range values {
			c.Request.PostForm[key][i] = sanitizer.Sanitize(value)
		}
	}
}

// sanitizeHeaders 过滤请求头
func sanitizeHeaders(c *gin.Context, sanitizer Sanitizer, skipHeaders map[string]bool) {
	for key, values := range c.Request.Header {
		if skipHeaders[key] {
			continue
		}
		for i, value := range values {
			c.Request.Header[key][i] = sanitizer.Sanitize(value)
		}
	}
}

// sanitizeJSONBody 过滤 JSON Body
func sanitizeJSONBody(c *gin.Context, sanitizer Sanitizer) {
	contentType := c.GetHeader("Content-Type")
	if contentType != "application/json" {
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil || len(bodyBytes) == 0 {
		return
	}

	var bodyData any
	if err := json.Unmarshal(bodyBytes, &bodyData); err != nil {
		// 不是有效的 JSON，恢复原始数据
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	// 递归过滤
	sanitized := sanitizeValue(bodyData, sanitizer)

	newBodyBytes, err := json.Marshal(sanitized)
	if err != nil {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewBuffer(newBodyBytes))
	c.Request.ContentLength = int64(len(newBodyBytes))
}

// sanitizeValue 递归过滤值
func sanitizeValue(v any, sanitizer Sanitizer) any {
	switch val := v.(type) {
	case string:
		return sanitizer.Sanitize(val)
	case map[string]any:
		result := make(map[string]any)
		for k, v := range val {
			result[k] = sanitizeValue(v, sanitizer)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = sanitizeValue(v, sanitizer)
		}
		return result
	default:
		return v
	}
}
