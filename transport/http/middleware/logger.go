package middleware

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kochabx/kit/log"
)

// LoggerConfig 日志中间件配置
type LoggerConfig struct {
	RequestBody  bool                     // 是否记录请求体
	ResponseBody bool                     // 是否记录响应体
	Header       bool                     // 是否记录请求头
	SkipPaths    []string                 // 跳过记录的路径
	SkipFunc     func(*http.Request) bool // 动态跳过判断函数
	Logger       *log.Logger              // 自定义日志记录器
}

// statusResponseWriter 包装 http.ResponseWriter 以捕获状态码和响应体
type statusResponseWriter struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if w.body != nil {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// clientIP 从请求中提取真实客户端 IP
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.Index(fwd, ","); idx != -1 {
			return strings.TrimSpace(fwd[:idx])
		}
		return fwd
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Logger 创建框架无关的请求日志中间件
func Logger(cfgs ...LoggerConfig) func(http.Handler) http.Handler {
	cfg := LoggerConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.SkipFunc) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()

			var requestBody []byte
			if cfg.RequestBody {
				body, err := io.ReadAll(r.Body)
				if err == nil {
					requestBody = body
					r.Body = io.NopCloser(bytes.NewReader(body))
				}
			}

			rw := &statusResponseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}
			if cfg.ResponseBody {
				rw.body = bytes.NewBuffer(nil)
			}

			next.ServeHTTP(rw, r)

			event := cfg.Logger.Info().
				Int("status", rw.status).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Dur("duration", time.Since(start)).
				Str("client_ip", clientIP(r))

			if query := r.URL.RawQuery; query != "" {
				event = event.Str("query", query)
			}

			if requestID := r.Header.Get("X-Request-Id"); requestID != "" {
				event = event.Str("request_id", requestID)
			}

			if cfg.Header {
				event = event.Any("headers", r.Header)
			}

			if cfg.RequestBody && len(requestBody) > 0 {
				event = event.Bytes("request_body", requestBody)
			}

			if cfg.ResponseBody && rw.body != nil {
				event = event.Bytes("response_body", rw.body.Bytes())
			}

			event.Send()
		})
	}
}
