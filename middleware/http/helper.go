package middleware

import (
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// prefixPath 预编译的前缀路径
type prefixPath struct {
	prefix    string // 原始前缀，如 "/api"
	prefixLen int    // 前缀长度
}

// PathMatcher 路径匹配器
type PathMatcher struct {
	exactPaths  map[string]struct{} // 精确匹配路径, 如 "/health"
	prefixPaths []prefixPath        // 前缀匹配路径, 如 "/api/**"
	patterns    []string            // glob 模式匹配, 如 "/api/*/users"
}

// NewPathMatcher 创建路径匹配器
// 支持三种匹配模式：
//   - 精确匹配："/health" 只匹配 "/health"
//   - 前缀匹配："/api/**" 匹配 "/api" 及其所有子路径
//   - Glob 模式："/api/*/users" 使用 path.Match 进行模式匹配
//
// Glob 模式语法（与 path.Match 一致）：
//   - '*' 匹配任意非 '/' 字符序列
//   - '?' 匹配任意单个非 '/' 字符
//   - '[abc]' 匹配括号内任意字符
//   - '[a-z]' 匹配范围内任意字符
func NewPathMatcher(paths []string) *PathMatcher {
	if len(paths) == 0 {
		return &PathMatcher{
			exactPaths: make(map[string]struct{}),
		}
	}

	pm := &PathMatcher{
		exactPaths:  make(map[string]struct{}, len(paths)),
		prefixPaths: make([]prefixPath, 0, len(paths)/2), // 预估一半用于前缀匹配
		patterns:    make([]string, 0, len(paths)/4),     // 预估四分之一用于 glob 模式
	}
	for _, p := range paths {
		if prefix, ok := strings.CutSuffix(p, "/**"); ok {
			// 前缀匹配模式：预编译前缀信息
			pm.prefixPaths = append(pm.prefixPaths, prefixPath{
				prefix:    prefix,
				prefixLen: len(prefix),
			})
		} else if strings.ContainsAny(p, "*?[") {
			// Glob 模式
			pm.patterns = append(pm.patterns, p)
		} else {
			// 精确匹配模式
			pm.exactPaths[p] = struct{}{}
		}
	}
	return pm
}

// Match 检查路径是否匹配
func (pm *PathMatcher) Match(urlPath string) bool {
	// 空匹配器快速返回
	if pm == nil {
		return false
	}

	// 1. 精确匹配检查 (O(1) 哈希查找，最快)
	if _, ok := pm.exactPaths[urlPath]; ok {
		return true
	}

	pathLen := len(urlPath)

	// 2. 前缀匹配检查
	for i := range pm.prefixPaths {
		pp := &pm.prefixPaths[i]
		if pathLen < pp.prefixLen {
			continue // 路径比前缀短，不可能匹配
		}
		// 精确匹配前缀本身
		if pathLen == pp.prefixLen {
			if urlPath == pp.prefix {
				return true
			}
			continue
		}
		// pathLen > pp.prefixLen: 使用 strings.HasPrefix 替代切片比较
		// 先检查分隔符位置，避免不必要的前缀比较
		if urlPath[pp.prefixLen] == '/' && strings.HasPrefix(urlPath, pp.prefix) {
			return true
		}
	}

	// 3. Glob 模式匹配 (最慢，放最后)
	for i := range pm.patterns {
		if matched, _ := path.Match(pm.patterns[i], urlPath); matched {
			return true
		}
	}
	return false
}

// shouldSkip 检查请求是否应跳过处理
func shouldSkip(c *gin.Context, matcher *PathMatcher, skipFunc func(*gin.Context) bool) bool {
	// 先检查自定义跳过函数（通常更轻量）
	if skipFunc != nil && skipFunc(c) {
		return true
	}
	// matcher.Match 内部已处理 nil 检查
	return matcher.Match(c.Request.URL.Path)
}
