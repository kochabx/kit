package middleware

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// prefixPath holds a precompiled path prefix.
type prefixPath struct {
	prefix    string // Original prefix, such as "/api".
	prefixLen int    // Prefix length.
}

// PathMatcher matches request paths.
type PathMatcher struct {
	exactPaths  map[string]struct{} // Exact paths, such as "/health".
	prefixPaths []prefixPath        // Prefix paths, such as "/api/**".
	patterns    []string            // Glob patterns, such as "/api/*/users".
}

// NewPathMatcher creates a path matcher.
// Three matching modes are supported:
//   - Exact: "/health" matches only "/health"
//   - Prefix: "/api/**" matches "/api" and its descendants
//   - Glob: "/api/*/users" uses path.Match
//
// Glob syntax follows path.Match:
//   - '*' matches any sequence of characters other than '/'
//   - '?' matches one character other than '/'
//   - '[abc]' matches one listed character
//   - '[a-z]' matches one character in the range
func NewPathMatcher(paths []string) *PathMatcher {
	if len(paths) == 0 {
		return &PathMatcher{
			exactPaths: make(map[string]struct{}),
		}
	}

	pm := &PathMatcher{
		exactPaths:  make(map[string]struct{}, len(paths)),
		prefixPaths: make([]prefixPath, 0, len(paths)/2), // Estimate that half of the paths are prefixes.
		patterns:    make([]string, 0, len(paths)/4),     // Estimate that a quarter of the paths are glob patterns.
	}
	for _, p := range paths {
		if prefix, ok := strings.CutSuffix(p, "/**"); ok {
			// Precompile a prefix path.
			pm.prefixPaths = append(pm.prefixPaths, prefixPath{
				prefix:    prefix,
				prefixLen: len(prefix),
			})
		} else if strings.ContainsAny(p, "*?[") {
			// Glob pattern.
			pm.patterns = append(pm.patterns, p)
		} else {
			// Exact path.
			pm.exactPaths[p] = struct{}{}
		}
	}
	return pm
}

// Match reports whether urlPath matches a configured rule.
func (pm *PathMatcher) Match(urlPath string) bool {
	// Return quickly for an empty matcher.
	if pm == nil {
		return false
	}

	// 1. Check exact paths with an O(1) map lookup.
	if _, ok := pm.exactPaths[urlPath]; ok {
		return true
	}

	pathLen := len(urlPath)

	// 2. Check prefixes.
	for i := range pm.prefixPaths {
		pp := &pm.prefixPaths[i]
		if pathLen < pp.prefixLen {
			continue // The path is shorter than the prefix and cannot match.
		}
		// Match the prefix itself.
		if pathLen == pp.prefixLen {
			if urlPath == pp.prefix {
				return true
			}
			continue
		}
		// For longer paths, use strings.HasPrefix rather than slicing.
		// Check the separator first to avoid unnecessary prefix comparisons.
		if urlPath[pp.prefixLen] == '/' && strings.HasPrefix(urlPath, pp.prefix) {
			return true
		}
	}

	// 3. Check glob patterns last.
	for i := range pm.patterns {
		if matched, _ := path.Match(pm.patterns[i], urlPath); matched {
			return true
		}
	}
	return false
}

// SkipConfig defines common middleware skip rules.
type SkipConfig struct {
	Paths []string                 // Paths that bypass the middleware.
	Func  func(*http.Request) bool // Dynamic skip predicate.
}

// shouldSkip reports whether the request should be skipped.
func shouldSkip(r *http.Request, matcher *PathMatcher, skipFunc func(*http.Request) bool) bool {
	if skipFunc != nil && skipFunc(r) {
		return true
	}
	return matcher.Match(r.URL.Path)
}

// AdaptToGin converts standard net/http middleware into a gin.HandlerFunc for routes or r.Use.
//
// Changes to *http.Request, including claims added to context, are propagated through
// c.Request = r so downstream handlers can read them from c.Request.Context().
//
// Example:
//
//	r := gin.New()
//	r.Use(AdaptToGin(Recovery()))
//	r.Use(AdaptToGin(Logger()))
//	r.Use(AdaptToGin(Auth(authCfg)))
func AdaptToGin(m func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		nextCalled := false
		m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			c.Request = r
			c.Next()
		})).ServeHTTP(
			c.Writer,
			c.Request,
		)
		if !nextCalled {
			c.Abort()
		}
	}
}
