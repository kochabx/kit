package defaults

import (
	"reflect"
	"testing"
	"time"
)

type tagMock struct {
	Name    string    `default:"John"`
	Age     int       `default:"18"`
	Hobby   []string  `default:"basketball,football"`
	Score   []int     `default:"90,80"`
	Height  []float64 `default:"180.5,170.5"`
	Enabled bool      `default:"true"`
	Weight  *float64  `default:"70.5"`
	Address struct {
		Province string `default:"New York"`
		City     string `default:"New York"`
	}
	Tags     map[string]string `default:"env:prod,region:us"`
	Duration time.Duration     `default:"1h30m"`
}

type Api struct {
	Name    string            `json:"name"`
	Method  string            `json:"method" default:"GET"`
	Url     string            `json:"url"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
	Timeout int               `json:"timeout" default:"3"`
	Period  int               `json:"period" default:"10"`
}

type mockWithSlice struct {
	Host    string  `json:"host"`
	Port    int     `json:"port" default:"8080"`
	Number  float64 `json:"number"`
	Enabled bool    `json:"enabled" default:"true"`
	Mock1   struct {
		Host string `json:"host" default:"localhost"`
	} `json:"mock1"`
	Mock2 struct {
		Number []int `json:"number"`
	} `json:"mock2"`
	Apis []Api `json:"apis"`
}

func TestApply(t *testing.T) {
	mock := &tagMock{Age: 20}

	if err := Apply(mock); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify values
	if mock.Name != "John" {
		t.Errorf("Expected Name=John, got %s", mock.Name)
	}
	if mock.Age != 20 {
		t.Errorf("Expected Age=20 (not overwritten), got %d", mock.Age)
	}
	if len(mock.Hobby) != 2 {
		t.Errorf("Expected 2 hobbies, got %d", len(mock.Hobby))
	}
	if !mock.Enabled {
		t.Error("Expected Enabled=true")
	}
	if mock.Weight == nil || *mock.Weight != 70.5 {
		t.Error("Expected Weight pointer to be set to 70.5")
	}
	if mock.Address.Province != "New York" {
		t.Errorf("Expected Province=New York, got %s", mock.Address.Province)
	}
	if len(mock.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(mock.Tags))
	}

	t.Logf("Result: %+v", mock)
}

func TestApplyWithSliceStruct(t *testing.T) {
	mock := &mockWithSlice{}

	// Initialize slice but don't set defaults
	mock.Apis = []Api{
		{
			Name: "test-api",
			Url:  "http://example.com/api/test",
		},
	}

	if err := Apply(mock); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify nested defaults were applied
	if mock.Port != 8080 {
		t.Errorf("Expected Port=8080, got %d", mock.Port)
	}
	if !mock.Enabled {
		t.Error("Expected Enabled=true")
	}
	if mock.Mock1.Host != "localhost" {
		t.Errorf("Expected Mock1.Host=localhost, got %s", mock.Mock1.Host)
	}
	if mock.Apis[0].Method != "GET" {
		t.Errorf("Expected Api Method=GET, got %s", mock.Apis[0].Method)
	}
	if mock.Apis[0].Timeout != 3 {
		t.Errorf("Expected Api Timeout=3, got %d", mock.Apis[0].Timeout)
	}

	t.Logf("Result: %+v", mock)
	t.Logf("Apis[0]: %+v", mock.Apis[0])
}

func TestApplyWithCustomTag(t *testing.T) {
	type CustomTag struct {
		Name string `mytag:"Alice"`
		Age  int    `mytag:"25"`
	}

	custom := &CustomTag{}
	if err := Apply(custom, WithTagName("mytag")); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if custom.Name != "Alice" {
		t.Errorf("Expected Name=Alice, got %s", custom.Name)
	}
	if custom.Age != 25 {
		t.Errorf("Expected Age=25, got %d", custom.Age)
	}
}

func TestApplyWithMaxDepth(t *testing.T) {
	type DeepNested struct {
		Level1 struct {
			Level2 struct {
				Level3 struct {
					Value string `default:"deep"`
				}
			}
		}
	}

	// Test with max depth that allows processing
	deep := &DeepNested{}
	if err := Apply(deep, WithMaxDepth(10)); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if deep.Level1.Level2.Level3.Value != "deep" {
		t.Errorf("Expected Value=deep, got %s", deep.Level1.Level2.Level3.Value)
	}

	// Test with max depth that prevents processing
	deep2 := &DeepNested{}
	err := Apply(deep2, WithMaxDepth(2))
	if err != ErrMaxDepthExceeded {
		t.Errorf("Expected ErrMaxDepthExceeded, got %v", err)
	}
}

func TestApplyErrors(t *testing.T) {
	tests := []struct {
		name   string
		target any
		want   error
	}{
		{
			name:   "not a pointer",
			target: tagMock{},
			want:   ErrTargetMustBePointer,
		},
		{
			name:   "nil pointer",
			target: (*tagMock)(nil),
			want:   ErrTargetIsNil,
		},
		{
			name:   "pointer to non-struct",
			target: new(int),
			want:   ErrUnsupportedType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Apply(tt.target)
			if err != tt.want {
				t.Errorf("Expected error %v, got %v", tt.want, err)
			}
		})
	}
}

func TestApplyWithCustomSeparator(t *testing.T) {
	type CustomSep struct {
		Values []int             `default:"1|2|3"`
		Tags   map[string]string `default:"a:1|b:2"`
	}

	custom := &CustomSep{}
	if err := Apply(custom, WithSeparator("|")); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify slice splitting uses custom separator
	if len(custom.Values) != 3 {
		t.Errorf("Expected 3 values, got %d", len(custom.Values))
	}
	if custom.Values[0] != 1 || custom.Values[1] != 2 || custom.Values[2] != 3 {
		t.Errorf("Expected [1,2,3], got %v", custom.Values)
	}

	// Verify map splitting uses custom separator
	if len(custom.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(custom.Tags))
	}
	if custom.Tags["a"] != "1" || custom.Tags["b"] != "2" {
		t.Errorf("Expected {a:1, b:2}, got %v", custom.Tags)
	}
}

func TestApplyWithFieldFilter(t *testing.T) {
	type Filtered struct {
		Public  string `default:"public"`
		private string `default:"private"`
		Skip    string `default:"skip" skip:"true"`
	}

	filtered := &Filtered{}

	// Filter out fields with "skip" tag
	filter := func(field reflect.StructField) bool {
		return field.Tag.Get("skip") != "true"
	}

	if err := Apply(filtered, WithFieldFilter(filter)); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if filtered.Public != "public" {
		t.Errorf("Expected Public=public, got %s", filtered.Public)
	}
	if filtered.Skip != "" {
		t.Errorf("Expected Skip to be empty (filtered), got %s", filtered.Skip)
	}
}

func TestApplyPointerScalar(t *testing.T) {
	type WithPointers struct {
		Name   *string  `default:"hello"`
		Count  *int     `default:"42"`
		Rate   *float64 `default:"3.14"`
		Active *bool    `default:"true"`
	}

	w := &WithPointers{}
	if err := Apply(w); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if w.Name == nil || *w.Name != "hello" {
		t.Errorf("Expected Name=hello, got %v", w.Name)
	}
	if w.Count == nil || *w.Count != 42 {
		t.Errorf("Expected Count=42, got %v", w.Count)
	}
	if w.Rate == nil || *w.Rate != 3.14 {
		t.Errorf("Expected Rate=3.14, got %v", w.Rate)
	}
	if w.Active == nil || !*w.Active {
		t.Errorf("Expected Active=true, got %v", w.Active)
	}
}

// TestApplyNilStructPointerWithoutDefaults verifies that a nil pointer to a struct
// with NO default tags stays nil (e.g. *tls.Config).
func TestApplyNilStructPointerWithoutDefaults(t *testing.T) {
	type ExternalConfig struct {
		ServerName string
		MinVersion uint16
	}

	type Config struct {
		Host string          `default:"localhost"`
		Port int             `default:"8080"`
		TLS  *ExternalConfig // no default tag, no defaults inside
	}

	cfg := &Config{}
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if cfg.Host != "localhost" {
		t.Errorf("Expected Host=localhost, got %s", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("Expected Port=8080, got %d", cfg.Port)
	}
	if cfg.TLS != nil {
		t.Errorf("Expected TLS to remain nil, got %+v", cfg.TLS)
	}
}

// TestApplyNilStructPointerWithDefaults verifies that a nil pointer to a struct
// WITH default tags IS initialized and defaults applied.
func TestApplyNilStructPointerWithDefaults(t *testing.T) {
	type NestedConfig struct {
		Host string `default:"127.0.0.1"`
		Port int    `default:"3306"`
	}

	type Config struct {
		Name   string        `default:"app"`
		Nested *NestedConfig // has default tags inside
	}

	cfg := &Config{}
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if cfg.Name != "app" {
		t.Errorf("Expected Name=app, got %s", cfg.Name)
	}
	if cfg.Nested == nil {
		t.Fatal("Expected Nested to be initialized")
	}
	if cfg.Nested.Host != "127.0.0.1" {
		t.Errorf("Expected Nested.Host=127.0.0.1, got %s", cfg.Nested.Host)
	}
	if cfg.Nested.Port != 3306 {
		t.Errorf("Expected Nested.Port=3306, got %d", cfg.Nested.Port)
	}
}

// Benchmark tests
func BenchmarkApply(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mock := &tagMock{}
		_ = Apply(mock)
	}
}

func BenchmarkApplyComplex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mock := &mockWithSlice{
			Apis: []Api{
				{Name: "api1", Url: "http://example.com"},
				{Name: "api2", Url: "http://example.org"},
			},
		}
		_ = Apply(mock)
	}
}

func BenchmarkApplyCached(b *testing.B) {
	// First call populates cache; subsequent calls benefit from cached struct metadata
	_ = Apply(&tagMock{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock := &tagMock{}
		_ = Apply(mock)
	}
}
