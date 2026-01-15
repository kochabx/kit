package http

import (
	"fmt"
	"net/url"
	"testing"
)

func TestNewURLBuilder(t *testing.T) {
	builder := NewURLBuilder().
		Scheme("http").
		Host("example.com").
		Port("0").
		Path("").
		Query("key", "value@example.com").
		Fragment("section").
		String()
	t.Log("builder: ", builder)
}

func TestFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		expected map[string]string
	}{
		{
			name:    "完整URL",
			input:   "https://example.com:8080/api/v1?param1=value1&param2=value2#section",
			wantErr: false,
			expected: map[string]string{
				"scheme":   "https",
				"host":     "example.com",
				"port":     "8080",
				"path":     "/api/v1",
				"fragment": "section",
			},
		},
		{
			name:    "简单URL",
			input:   "http://localhost/test",
			wantErr: false,
			expected: map[string]string{
				"scheme": "http",
				"host":   "localhost",
				"path":   "/test",
			},
		},
		{
			name:    "无效URL",
			input:   "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := FromURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if builder.scheme != tt.expected["scheme"] {
				t.Errorf("scheme = %v, want %v", builder.scheme, tt.expected["scheme"])
			}
			if builder.host != tt.expected["host"] {
				t.Errorf("host = %v, want %v", builder.host, tt.expected["host"])
			}
			if builder.port != tt.expected["port"] {
				t.Errorf("port = %v, want %v", builder.port, tt.expected["port"])
			}
			if builder.path.String() != tt.expected["path"] {
				t.Errorf("path = %v, want %v", builder.path.String(), tt.expected["path"])
			}
			if builder.fragment != tt.expected["fragment"] {
				t.Errorf("fragment = %v, want %v", builder.fragment, tt.expected["fragment"])
			}
		})
	}
}

func TestURLBuilder_Scheme(t *testing.T) {
	builder := NewURLBuilder()
	result := builder.Scheme("https")

	if result != builder {
		t.Error("Scheme() 应该返回同一个构建器实例")
	}
	if builder.scheme != "https" {
		t.Errorf("scheme = %v, want https", builder.scheme)
	}
}

func TestURLBuilder_Host(t *testing.T) {
	builder := NewURLBuilder()
	result := builder.Host("example.com")

	if result != builder {
		t.Error("Host() 应该返回同一个构建器实例")
	}
	if builder.host != "example.com" {
		t.Errorf("host = %v, want example.com", builder.host)
	}
}

func TestURLBuilder_Port(t *testing.T) {
	builder := NewURLBuilder()
	result := builder.Port("8080")

	if result != builder {
		t.Error("Port() 应该返回同一个构建器实例")
	}
	if builder.port != "8080" {
		t.Errorf("port = %v, want 8080", builder.port)
	}
}

func TestURLBuilder_Path(t *testing.T) {
	builder := NewURLBuilder()
	result := builder.Path("/api/v1")

	if result != builder {
		t.Error("Path() 应该返回同一个构建器实例")
	}
	if builder.path.String() != "/api/v1" {
		t.Errorf("path = %v, want /api/v1", builder.path.String())
	}

	// 测试覆盖路径
	builder.Path("/new/path")
	if builder.path.String() != "/new/path" {
		t.Errorf("path = %v, want /new/path", builder.path.String())
	}

	// 测试空路径
	builder.Path("")
	if builder.path.String() != "" {
		t.Errorf("path = %v, want empty", builder.path.String())
	}
}

func TestURLBuilder_AppendPath(t *testing.T) {
	tests := []struct {
		name     string
		initial  string
		segments []string
		expected string
	}{
		{
			name:     "空初始路径",
			initial:  "",
			segments: []string{"api", "v1"},
			expected: "api/v1",
		},
		{
			name:     "已有路径追加",
			initial:  "/api",
			segments: []string{"v1", "users"},
			expected: "/api/v1/users",
		},
		{
			name:     "包含空段",
			initial:  "/api",
			segments: []string{"", "v1", "", "users"},
			expected: "/api/v1/users",
		},
		{
			name:     "无段追加",
			initial:  "/api",
			segments: []string{},
			expected: "/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewURLBuilder().Path(tt.initial)
			result := builder.AppendPath(tt.segments...)

			if result != builder {
				t.Error("AppendPath() 应该返回同一个构建器实例")
			}
			if builder.path.String() != tt.expected {
				t.Errorf("path = %v, want %v", builder.path.String(), tt.expected)
			}
		})
	}
}

func TestURLBuilder_Query(t *testing.T) {
	builder := NewURLBuilder()
	result := builder.Query("key1", "value1")

	if result != builder {
		t.Error("Query() 应该返回同一个构建器实例")
	}

	values := builder.query.Get("key1")
	if values != "value1" {
		t.Errorf("query key1 = %v, want value1", values)
	}

	// 测试同一键多个值
	builder.Query("key1", "value2")
	allValues := builder.query["key1"]
	if len(allValues) != 2 {
		t.Errorf("query key1 values count = %v, want 2", len(allValues))
	}
}

func TestURLBuilder_SetQuery(t *testing.T) {
	builder := NewURLBuilder()
	builder.Query("key1", "value1")
	builder.Query("key1", "value2")

	result := builder.SetQuery("key1", "newvalue")

	if result != builder {
		t.Error("SetQuery() 应该返回同一个构建器实例")
	}

	values := builder.query["key1"]
	if len(values) != 1 || values[0] != "newvalue" {
		t.Errorf("query key1 = %v, want [newvalue]", values)
	}
}

func TestURLBuilder_QueryMap(t *testing.T) {
	builder := NewURLBuilder()
	params := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	result := builder.QueryMap(params)

	if result != builder {
		t.Error("QueryMap() 应该返回同一个构建器实例")
	}

	for k, v := range params {
		if builder.query.Get(k) != v {
			t.Errorf("query %s = %v, want %v", k, builder.query.Get(k), v)
		}
	}
}

func TestURLBuilder_SetQueryMap(t *testing.T) {
	builder := NewURLBuilder()
	builder.Query("key1", "oldvalue")

	params := map[string]string{
		"key1": "newvalue",
		"key2": "value2",
	}

	result := builder.SetQueryMap(params)

	if result != builder {
		t.Error("SetQueryMap() 应该返回同一个构建器实例")
	}

	if builder.query.Get("key1") != "newvalue" {
		t.Errorf("query key1 = %v, want newvalue", builder.query.Get("key1"))
	}
	if builder.query.Get("key2") != "value2" {
		t.Errorf("query key2 = %v, want value2", builder.query.Get("key2"))
	}
}

func TestURLBuilder_QuerySlice(t *testing.T) {
	builder := NewURLBuilder()
	values := []string{"value1", "value2", "value3"}

	result := builder.QuerySlice("key1", values)

	if result != builder {
		t.Error("QuerySlice() 应该返回同一个构建器实例")
	}

	queryValues := builder.query["key1"]
	if len(queryValues) != len(values) {
		t.Errorf("query values count = %v, want %v", len(queryValues), len(values))
	}

	for i, v := range values {
		if queryValues[i] != v {
			t.Errorf("query value[%d] = %v, want %v", i, queryValues[i], v)
		}
	}
}

func TestURLBuilder_Fragment(t *testing.T) {
	builder := NewURLBuilder()
	result := builder.Fragment("section1")

	if result != builder {
		t.Error("Fragment() 应该返回同一个构建器实例")
	}
	if builder.fragment != "section1" {
		t.Errorf("fragment = %v, want section1", builder.fragment)
	}
}

func TestURLBuilder_Build(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *URLBuilder
		expected  string
	}{
		{
			name: "完整URL",
			setupFunc: func() *URLBuilder {
				return NewURLBuilder().
					Scheme("https").
					Host("example.com").
					Port("8080").
					Path("/api/v1").
					Query("param1", "value1").
					Query("param2", "value2").
					Fragment("section")
			},
			expected: "https://example.com:8080/api/v1?param1=value1&param2=value2#section",
		},
		{
			name: "最小URL",
			setupFunc: func() *URLBuilder {
				return NewURLBuilder().
					Scheme("http").
					Host("localhost")
			},
			expected: "http://localhost",
		},
		{
			name: "无端口",
			setupFunc: func() *URLBuilder {
				return NewURLBuilder().
					Scheme("https").
					Host("example.com").
					Path("/path")
			},
			expected: "https://example.com/path",
		},
		{
			name: "多值查询参数",
			setupFunc: func() *URLBuilder {
				return NewURLBuilder().
					Scheme("http").
					Host("test.com").
					Query("filter", "type1").
					Query("filter", "type2")
			},
			expected: "http://test.com?filter=type1&filter=type2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.setupFunc()
			result, err := builder.Build()

			if err != nil {
				t.Errorf("Build() error = %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Build() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestURLBuilder_MustBuild(t *testing.T) {
	builder := NewURLBuilder().
		Scheme("https").
		Host("example.com").
		Path("/test")

	result := builder.MustBuild()
	expected := "https://example.com/test"

	if result != expected {
		t.Errorf("MustBuild() = %v, want %v", result, expected)
	}
}

func TestURLBuilder_String(t *testing.T) {
	builder := NewURLBuilder().
		Scheme("https").
		Host("example.com").
		Path("/test")

	result := builder.String()
	expected := "https://example.com/test"

	if result != expected {
		t.Errorf("String() = %v, want %v", result, expected)
	}
}

func TestURLBuilder_Reset(t *testing.T) {
	builder := NewURLBuilder().
		Scheme("https").
		Host("example.com").
		Port("8080").
		Path("/api").
		Query("key", "value").
		Fragment("section")

	result := builder.Reset()

	if result != builder {
		t.Error("Reset() 应该返回同一个构建器实例")
	}

	if builder.scheme != "" {
		t.Errorf("scheme = %v, want empty", builder.scheme)
	}
	if builder.host != "" {
		t.Errorf("host = %v, want empty", builder.host)
	}
	if builder.port != "" {
		t.Errorf("port = %v, want empty", builder.port)
	}
	if builder.path.String() != "" {
		t.Errorf("path = %v, want empty", builder.path.String())
	}
	if len(builder.query) != 0 {
		t.Errorf("query length = %v, want 0", len(builder.query))
	}
	if builder.fragment != "" {
		t.Errorf("fragment = %v, want empty", builder.fragment)
	}
}

func TestURLBuilder_Clone(t *testing.T) {
	original := NewURLBuilder().
		Scheme("https").
		Host("example.com").
		Port("8080").
		Path("/api").
		Query("key1", "value1").
		Query("key1", "value2").
		Fragment("section")

	clone := original.Clone()

	// 验证克隆的值
	if clone.scheme != original.scheme {
		t.Errorf("clone.scheme = %v, want %v", clone.scheme, original.scheme)
	}
	if clone.host != original.host {
		t.Errorf("clone.host = %v, want %v", clone.host, original.host)
	}
	if clone.port != original.port {
		t.Errorf("clone.port = %v, want %v", clone.port, original.port)
	}
	if clone.path.String() != original.path.String() {
		t.Errorf("clone.path = %v, want %v", clone.path.String(), original.path.String())
	}
	if clone.fragment != original.fragment {
		t.Errorf("clone.fragment = %v, want %v", clone.fragment, original.fragment)
	}

	// 验证查询参数深拷贝
	originalValues := original.query["key1"]
	cloneValues := clone.query["key1"]
	if len(cloneValues) != len(originalValues) {
		t.Errorf("clone query values count = %v, want %v", len(cloneValues), len(originalValues))
	}

	// 修改克隆不应影响原始
	clone.Query("key1", "newvalue")
	if len(original.query["key1"]) != 2 {
		t.Error("修改克隆影响了原始对象")
	}
	if len(clone.query["key1"]) != 3 {
		t.Error("克隆修改失败")
	}
}

func TestBuildHost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		expected string
	}{
		{
			name:     "主机和端口",
			host:     "example.com",
			port:     "8080",
			expected: "example.com:8080",
		},
		{
			name:     "只有主机",
			host:     "example.com",
			port:     "",
			expected: "example.com",
		},
		{
			name:     "空主机",
			host:     "",
			port:     "8080",
			expected: "",
		},
		{
			name:     "都为空",
			host:     "",
			port:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := &URLBuilder{
				host: tt.host,
				port: tt.port,
			}
			result := builder.buildHost()
			if result != tt.expected {
				t.Errorf("buildHost() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildHTTP(t *testing.T) {
	builder := BuildHTTP("example.com", "api", "v1")
	result := builder.MustBuild()
	expected := "http://example.com/api/v1"

	if result != expected {
		t.Errorf("BuildHTTP() = %v, want %v", result, expected)
	}
}

func TestBuildHTTPS(t *testing.T) {
	builder := BuildHTTPS("example.com", "api", "v1")
	result := builder.MustBuild()
	expected := "https://example.com/api/v1"

	if result != expected {
		t.Errorf("BuildHTTPS() = %v, want %v", result, expected)
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		segments []string
		expected string
	}{
		{
			name:     "基本路径拼接",
			base:     "/api",
			segments: []string{"v1", "users"},
			expected: "/api/v1/users",
		},
		{
			name:     "空基础路径",
			base:     "",
			segments: []string{"api", "v1"},
			expected: "api/v1",
		},
		{
			name:     "无段",
			base:     "/api",
			segments: []string{},
			expected: "/api",
		},
		{
			name:     "包含空段",
			base:     "/api",
			segments: []string{"", "v1", "", "users"},
			expected: "/api/v1/users",
		},
		{
			name:     "都为空",
			base:     "",
			segments: []string{"", ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Join(tt.base, tt.segments...)
			if result != tt.expected {
				t.Errorf("Join() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// 基准测试
func BenchmarkURLBuilder_Build(b *testing.B) {
	builder := NewURLBuilder().
		Scheme("https").
		Host("example.com").
		Port("8080").
		Path("/api/v1").
		Query("param1", "value1").
		Query("param2", "value2").
		Fragment("section")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = builder.Build()
	}
}

func BenchmarkURLBuilder_AppendPath(b *testing.B) {
	builder := NewURLBuilder().Path("/base")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder.AppendPath("segment")
		builder.path.Reset()
		builder.path.WriteString("/base")
	}
}

func BenchmarkFromURL(b *testing.B) {
	testURL := "https://example.com:8080/api/v1?param1=value1&param2=value2#section"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FromURL(testURL)
	}
}

// 示例测试
func ExampleURLBuilder() {
	// 构建一个完整的API URL
	url := NewURLBuilder().
		Scheme("https").
		Host("api.example.com").
		Port("443").
		AppendPath("v1", "users").
		Query("page", "1").
		Query("limit", "10").
		Fragment("results").
		MustBuild()

	fmt.Println(url)
	// Output: https://api.example.com:443/v1/users?limit=10&page=1#results
}

func ExampleBuildHTTPS() {
	// 快速构建HTTPS URL
	url := BuildHTTPS("api.example.com", "v1", "users").
		Query("active", "true").
		MustBuild()

	fmt.Println(url)
	// Output: https://api.example.com/v1/users?active=true
}

func ExampleFromURL() {
	// 从现有URL创建构建器并修改
	builder, _ := FromURL("https://example.com/old/path?old=param")

	// 清空查询参数并设置新的
	builder.query = make(url.Values)
	newURL := builder.
		Path("/new/path").
		SetQuery("new", "param").
		MustBuild()

	fmt.Println(newURL)
	// Output: https://example.com/new/path?new=param
}
