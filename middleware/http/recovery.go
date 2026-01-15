package middleware

import (
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type RecoveryConfig struct {
	Stack bool
}

func GinRecovery() gin.HandlerFunc {
	return GinRecoveryWithConfig(RecoveryConfig{
		Stack: true,
	})
}

func GinRecoveryWithConfig(config RecoveryConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				httpRequest, _ := httputil.DumpRequest(c.Request, false)
				// Check for a broken connection, as it is not really a
				// condition that warrants a panic stack trace.
				brokenPipe := isBrokenPipe(err)

				if brokenPipe {
					log.Error().AnErr("recovery", err.(error)).Bytes("request", httpRequest).Msg("broken pipe error")
					// If the connection is dead, we can't write a status to it.
					_ = c.Error(err.(error)) // nolint: err check
					c.Abort()
					return
				}

				if config.Stack {
					log.Error().AnErr("recovery", err.(error)).Bytes("request", httpRequest).Msg("recovered from panic")
				} else {
					log.Error().AnErr("recovery", err.(error)).Bytes("request", httpRequest).Msg("recovered from panic")
				}
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

func isBrokenPipe(err any) bool {
	if ne, ok := err.(*net.OpError); ok {
		if se, ok := ne.Err.(*os.SyscallError); ok {
			errStr := strings.ToLower(se.Error())
			return strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "connection reset by peer")
		}
	}
	return false
}
