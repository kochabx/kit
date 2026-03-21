package middleware

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"net/http"

	"github.com/kochabx/kit/log"
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
	QueryEnabled  bool                     // 过滤 Query 参数
	FormEnabled   bool                     // 过滤表单数据
	HeaderEnabled bool                     // 过滤请求头（慎用）
	BodyEnabled   bool                     // 过滤 JSON Body
	Sanitizer     Sanitizer                // 自定义过滤器
	SkipPaths     []string                 // 跳过处理的路径前缀
	SkipFunc      func(*http.Request) bool // 动态跳过判断函数
	SkipHeaders   []string                 // 跳过过滤的请求头
	Logger        *log.Logger              // 自定义日志记录器
}

// Xss 创建框架无关的 XSS 防护中间件
func Xss(cfgs ...XssConfig) func(http.Handler) http.Handler {
	cfg := XssConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	if cfg.Sanitizer == nil {
		cfg.Sanitizer = HTMLEscapeSanitizer()
	}

	skipHeaderMap := make(map[string]bool, len(cfg.SkipHeaders))
	for _, h := range cfg.SkipHeaders {
		skipHeaderMap[h] = true
	}

	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.SkipFunc) {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.QueryEnabled {
				sanitizeQuery(r, cfg.Sanitizer)
			}

			if cfg.FormEnabled {
				sanitizeForm(r, cfg.Sanitizer)
			}

			if cfg.HeaderEnabled {
				sanitizeHeaders(r, cfg.Sanitizer, skipHeaderMap)
			}

			if cfg.BodyEnabled {
				sanitizeJSONBody(r, cfg.Sanitizer)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func sanitizeQuery(r *http.Request, sanitizer Sanitizer) {
	query := r.URL.Query()
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
		r.URL.RawQuery = query.Encode()
	}
}

func sanitizeForm(r *http.Request, sanitizer Sanitizer) {
	if err := r.ParseForm(); err != nil {
		return
	}

	for key, values := range r.PostForm {
		for i, value := range values {
			r.PostForm[key][i] = sanitizer.Sanitize(value)
		}
	}
}

func sanitizeHeaders(r *http.Request, sanitizer Sanitizer, skipHeaders map[string]bool) {
	for key, values := range r.Header {
		if skipHeaders[key] {
			continue
		}
		for i, value := range values {
			r.Header[key][i] = sanitizer.Sanitize(value)
		}
	}
}

func sanitizeJSONBody(r *http.Request, sanitizer Sanitizer) {
	if r.Header.Get("Content-Type") != "application/json" {
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil || len(bodyBytes) == 0 {
		return
	}

	var bodyData any
	if err := json.Unmarshal(bodyBytes, &bodyData); err != nil {
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	sanitized := sanitizeValue(bodyData, sanitizer)

	newBodyBytes, err := json.Marshal(sanitized)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	r.Body = io.NopCloser(bytes.NewBuffer(newBodyBytes))
	r.ContentLength = int64(len(newBodyBytes))
}

func sanitizeValue(v any, sanitizer Sanitizer) any {
	switch val := v.(type) {
	case string:
		return sanitizer.Sanitize(val)
	case map[string]any:
		result := make(map[string]any, len(val))
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
