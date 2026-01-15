package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/core/util"
	"github.com/kochabx/kit/errors"
	klog "github.com/kochabx/kit/log"
)

var (
	ErrorUnauthorized = errors.Unauthorized("unauthorized")
	ErrorForbidden    = errors.Forbidden("forbidden")
	ErrorCrypto       = errors.Internal("decrypt request body failed")
	ErrorSignature    = errors.Internal("verify signature failed")
)

var (
	log = klog.New()
)

func SetLogger(logger *klog.Logger) {
	log = logger
}

func skippedPathPrefixes(c *gin.Context, prefixes ...string) bool {
	if len(prefixes) == 0 {
		return false
	}

	path := c.Request.URL.Path
	for _, prefix := range prefixes {
		if path == prefix || len(path) > len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

func ctxAccountInfo(c *gin.Context) (id int64, roles []string, err error) {
	ctx := c.Request.Context()
	id, err = util.CtxValue[int64](ctx, "id")
	if err != nil {
		return
	}
	roles, err = util.CtxValue[[]string](ctx, "roles")
	if err != nil {
		return
	}
	return
}
