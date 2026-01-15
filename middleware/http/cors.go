package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type CorsConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	ExposeHeaders    []string
	MaxAge           int
}

func Cors() gin.HandlerFunc {
	return CorsWithConfig(CorsConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization"},
		AllowCredentials: true,
		ExposeHeaders:    []string{},
		MaxAge:           43200,
	})
}

func CorsWithConfig(config CorsConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if !slices.Contains(config.AllowOrigins, origin) {
			c.Next()
			return
		}

		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ","))
		c.Writer.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ","))
		c.Writer.Header().Set("Access-Control-Allow-Credentials", strconv.FormatBool(config.AllowCredentials))
		c.Writer.Header().Set("Access-Control-Expose-Headers", strings.Join(config.ExposeHeaders, ","))
		c.Writer.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
