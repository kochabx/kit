package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/transport/http"
)

type AuthConfig struct {
	// SkippedPathPrefixes is a list of path prefixes that should be skipped from auth
	SkippedPathPrefixes []string
	// Validate is a function that takes a gin context and returns the auth value
	// The fields that must be included are: id, roles
	// struct{} or a type that implements the String() method
	Validate func(c *gin.Context) (map[any]any, error)
}

func AuthWithConfig(config AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.Validate == nil || skippedPathPrefixes(c, config.SkippedPathPrefixes...) {
			c.Next()
			return
		}

		// validate
		result, err := config.Validate(c)
		if err != nil {
			http.GinJSONE(c, http.StatusUnauthorized, err)
			return
		}

		// with context
		ctx := c.Request.Context()
		for k, v := range result {
			ctx = context.WithValue(ctx, k, v)
		}
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
