package http

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

// 提供链式调用API用于构建复杂URL
type URLBuilder struct {
	scheme   string
	host     string
	port     string
	path     strings.Builder
	query    url.Values
	fragment string
}

// NewURLBuilder 创建新的URL构建器实例
func NewURLBuilder() *URLBuilder {
	return &URLBuilder{
		query: make(url.Values),
	}
}

// Scheme 设置URL协议 (http, https, ftp等)
func (b *URLBuilder) Scheme(scheme string) *URLBuilder {
	b.scheme = scheme
	return b
}

// Host 设置主机地址
func (b *URLBuilder) Host(host string) *URLBuilder {
	b.host = host
	return b
}

// Port 设置端口号
func (b *URLBuilder) Port(port string) *URLBuilder {
	if port != "" && port != "0" {
		b.port = port
	}
	return b
}

// Path 设置完整路径，会覆盖之前的路径
func (b *URLBuilder) Path(path string) *URLBuilder {
	if path != "" {
		b.path.Reset()
		b.path.WriteString(path)
	}
	return b
}

// AppendPath 追加路径段，自动处理斜杠
func (b *URLBuilder) AppendPath(segments ...string) *URLBuilder {
	if len(segments) == 0 {
		return b
	}

	currentPath := b.path.String()

	// 构建新路径
	var pathSegments []string
	if currentPath != "" {
		pathSegments = append(pathSegments, currentPath)
	}

	// 过滤空段
	for _, segment := range segments {
		if segment != "" {
			pathSegments = append(pathSegments, segment)
		}
	}

	if len(pathSegments) > 0 {
		b.path.Reset()
		b.path.WriteString(path.Join(pathSegments...))
	}

	return b
}

// Query 添加单个查询参数
func (b *URLBuilder) Query(key, value string) *URLBuilder {
	b.query.Add(key, value)
	return b
}

// SetQuery 设置查询参数，会覆盖同名参数
func (b *URLBuilder) SetQuery(key, value string) *URLBuilder {
	b.query.Set(key, value)
	return b
}

// QueryMap 批量添加查询参数
func (b *URLBuilder) QueryMap(params map[string]string) *URLBuilder {
	for k, v := range params {
		b.query.Add(k, v)
	}
	return b
}

// SetQueryMap 批量设置查询参数，会覆盖同名参数
func (b *URLBuilder) SetQueryMap(params map[string]string) *URLBuilder {
	for k, v := range params {
		b.query.Set(k, v)
	}
	return b
}

// QuerySlice 添加多值查询参数
func (b *URLBuilder) QuerySlice(key string, values []string) *URLBuilder {
	for _, value := range values {
		b.query.Add(key, value)
	}
	return b
}

// Fragment 设置URL片段标识符 (#后面的部分)
func (b *URLBuilder) Fragment(fragment string) *URLBuilder {
	b.fragment = fragment
	return b
}

// Build 构建最终的URL字符串
func (b *URLBuilder) Build() (string, error) {
	u := &url.URL{
		Scheme: b.scheme,
		Host:   b.buildHost(),
		Path:   b.path.String(),
	}

	if len(b.query) > 0 {
		u.RawQuery = b.query.Encode()
	}

	if b.fragment != "" {
		u.Fragment = b.fragment
	}

	return u.String(), nil
}

// MustBuild 构建URL，如果出错则panic
func (b *URLBuilder) MustBuild() string {
	result, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("构建URL失败: %v", err))
	}
	return result
}

// String 实现fmt.Stringer接口
func (b *URLBuilder) String() string {
	result, _ := b.Build()
	return result
}

// Reset 重置构建器到初始状态
func (b *URLBuilder) Reset() *URLBuilder {
	b.scheme = ""
	b.host = ""
	b.port = ""
	b.path.Reset()
	b.query = make(url.Values)
	b.fragment = ""
	return b
}

// Clone 深拷贝构建器
func (b *URLBuilder) Clone() *URLBuilder {
	newBuilder := &URLBuilder{
		scheme:   b.scheme,
		host:     b.host,
		port:     b.port,
		fragment: b.fragment,
		query:    make(url.Values),
	}

	// 拷贝路径
	newBuilder.path.WriteString(b.path.String())

	// 深拷贝查询参数
	for k, v := range b.query {
		newBuilder.query[k] = slices.Clone(v)
	}

	return newBuilder
}

// buildHost 构建主机部分，包含端口
func (b *URLBuilder) buildHost() string {
	if b.host == "" {
		return ""
	}

	if b.port == "" {
		return b.host
	}

	return b.host + ":" + b.port
}

// BuildHTTP 快速构建HTTP URL
func BuildHTTP(host string, pathSegments ...string) *URLBuilder {
	return NewURLBuilder().Scheme("http").Host(host).AppendPath(pathSegments...)
}

// BuildHTTPS 快速构建HTTPS URL
func BuildHTTPS(host string, pathSegments ...string) *URLBuilder {
	return NewURLBuilder().Scheme("https").Host(host).AppendPath(pathSegments...)
}

// FromURL 从现有URL字符串创建构建器
func FromURL(rawURL string) (*URLBuilder, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("解析URL失败: %w", err)
	}

	builder := &URLBuilder{
		scheme:   u.Scheme,
		host:     u.Hostname(),
		port:     u.Port(),
		fragment: u.Fragment,
		query:    u.Query(),
	}

	if u.Path != "" {
		builder.path.WriteString(u.Path)
	}

	return builder, nil
}

// Join 简单的路径拼接函数
func Join(base string, segments ...string) string {
	if len(segments) == 0 {
		return base
	}

	allPaths := make([]string, 0, len(segments)+1)
	if base != "" {
		allPaths = append(allPaths, base)
	}

	for _, segment := range segments {
		if segment != "" {
			allPaths = append(allPaths, segment)
		}
	}

	return path.Join(allPaths...)
}
