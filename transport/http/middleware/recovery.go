package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"

	"github.com/kochabx/kit/log"
)

// RecoveryConfig Recovery 中间件配置
type RecoveryConfig struct {
	StackTrace bool        // 是否记录堆栈信息
	Logger     *log.Logger // 自定义日志记录器
}

// Recovery 创建 panic 恢复中间件
func Recovery(cfgs ...RecoveryConfig) func(http.Handler) http.Handler {
	cfg := RecoveryConfig{
		StackTrace: true,
	}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					httpRequest, _ := httputil.DumpRequest(r, false)

					if isBrokenPipe(err) {
						cfg.Logger.Warn().
							Str("error", fmt.Sprintf("%v", err)).
							Bytes("request", httpRequest).
							Msg("broken pipe")
						return
					}

					event := cfg.Logger.Error().
						Str("error", fmt.Sprintf("%v", err)).
						Bytes("request", httpRequest)

					if cfg.StackTrace {
						event = event.Bytes("stack", debug.Stack())
					}

					event.Msg("panic recovered")
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// isBrokenPipe 检查是否为断开的连接错误
func isBrokenPipe(err any) bool {
	if ne, ok := err.(*net.OpError); ok {
		if se, ok := ne.Err.(*os.SyscallError); ok {
			errStr := strings.ToLower(se.Error())
			return strings.Contains(errStr, "broken pipe") ||
				strings.Contains(errStr, "connection reset by peer")
		}
	}
	return false
}
