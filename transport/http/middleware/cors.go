package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CorsConfig configures CORS middleware.
type CorsConfig struct {
	Skip             SkipConfig // Skip rules.
	AllowCredentials bool       // Whether to allow credentials.
	MaxAge           int        // Preflight cache duration in seconds.
	AllowOrigins     []string   // Allowed origins, supporting "*" wildcards.
	AllowMethods     []string   // Allowed HTTP methods.
	AllowHeaders     []string   // Allowed request headers.
	ExposeHeaders    []string   // Response headers exposed to clients.
}

// DefaultCorsConfig returns the default CORS configuration.
func DefaultCorsConfig() CorsConfig {
	return CorsConfig{
		AllowCredentials: false, // AllowCredentials must be false when AllowOrigins is "*".
		MaxAge:           43200,
		ExposeHeaders:    []string{},
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Accept-Encoding",
			"X-CSRF-Token",
			"Authorization",
			"X-Request-Id",
		},
	}
}

// Cors creates CORS middleware.
func Cors(cfgs ...CorsConfig) func(http.Handler) http.Handler {
	cfg := DefaultCorsConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	allowAllOrigins := len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*"
	methodsHeader := strings.Join(cfg.AllowMethods, ", ")
	headersHeader := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeader := strings.Join(cfg.ExposeHeaders, ", ")
	maxAgeHeader := strconv.Itoa(cfg.MaxAge)
	credentialsHeader := strconv.FormatBool(cfg.AllowCredentials)

	matcher := NewPathMatcher(cfg.Skip.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.Skip.Func) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := allowAllOrigins || isOriginAllowed(origin, cfg.AllowOrigins)
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			if allowAllOrigins && !cfg.AllowCredentials {
				header.Set("Access-Control-Allow-Origin", "*")
			} else {
				header.Set("Access-Control-Allow-Origin", origin)
				header.Set("Vary", "Origin")
			}

			header.Set("Access-Control-Allow-Methods", methodsHeader)
			header.Set("Access-Control-Allow-Headers", headersHeader)
			header.Set("Access-Control-Allow-Credentials", credentialsHeader)

			if exposeHeader != "" {
				header.Set("Access-Control-Expose-Headers", exposeHeader)
			}
			header.Set("Access-Control-Max-Age", maxAgeHeader)

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks whether an origin is allowed.
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == origin {
			return true
		}
		// Wildcard patterns such as "*.example.com" are supported.
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // ".example.com"
			if strings.HasSuffix(origin, suffix) {
				return true
			}
		}
	}
	return false
}
