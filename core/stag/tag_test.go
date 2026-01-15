package stag

import (
	"reflect"
	"testing"
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
	Tags map[string]string `default:"env:prod,region:us"`
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

func TestApplyDefaults(t *testing.T) {
	mock := &tagMock{Age: 20}

	if err := ApplyDefaults(mock); err != nil {
		t.Fatalf("ApplyDefaults failed: %v", err)
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

func TestApplyDefaultsWithSliceStruct(t *testing.T) {
	mock := &mockWithSlice{}

	// Initialize slice but don't set defaults
	mock.Apis = []Api{
		{
			Name: "test-api",
			Url:  "http://example.com/api/test",
		},
	}

	if err := ApplyDefaults(mock); err != nil {
		t.Fatalf("ApplyDefaults failed: %v", err)
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

func TestApplyDefaultsWithCustomTag(t *testing.T) {
	type CustomTag struct {
		Name string `mytag:"Alice"`
		Age  int    `mytag:"25"`
	}

	custom := &CustomTag{}
	if err := ApplyDefaults(custom, WithTagName("mytag")); err != nil {
		t.Fatalf("ApplyDefaults failed: %v", err)
	}

	if custom.Name != "Alice" {
		t.Errorf("Expected Name=Alice, got %s", custom.Name)
	}
	if custom.Age != 25 {
		t.Errorf("Expected Age=25, got %d", custom.Age)
	}
}

func TestApplyDefaultsWithMaxDepth(t *testing.T) {
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
	if err := ApplyDefaults(deep, WithMaxDepth(10)); err != nil {
		t.Fatalf("ApplyDefaults failed: %v", err)
	}
	if deep.Level1.Level2.Level3.Value != "deep" {
		t.Errorf("Expected Value=deep, got %s", deep.Level1.Level2.Level3.Value)
	}

	// Test with max depth that prevents processing
	deep2 := &DeepNested{}
	err := ApplyDefaults(deep2, WithMaxDepth(2))
	if err != ErrMaxDepthExceeded {
		t.Errorf("Expected ErrMaxDepthExceeded, got %v", err)
	}
}

func TestApplyDefaultsErrors(t *testing.T) {
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
			err := ApplyDefaults(tt.target)
			if err != tt.want {
				t.Errorf("Expected error %v, got %v", tt.want, err)
			}
		})
	}
}

func TestApplyDefaultsWithCustomSeparator(t *testing.T) {
	type CustomSep struct {
		Values []int `default:"1|2|3"`
	}

	custom := &CustomSep{}
	if err := ApplyDefaults(custom, WithSeparator("|")); err != nil {
		t.Fatalf("ApplyDefaults failed: %v", err)
	}

	if len(custom.Values) != 3 {
		t.Errorf("Expected 3 values, got %d", len(custom.Values))
	}
	if custom.Values[0] != 1 || custom.Values[1] != 2 || custom.Values[2] != 3 {
		t.Errorf("Expected [1,2,3], got %v", custom.Values)
	}
}

func TestApplyDefaultsWithFieldFilter(t *testing.T) {
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

	if err := ApplyDefaults(filtered, WithFieldFilter(filter)); err != nil {
		t.Fatalf("ApplyDefaults failed: %v", err)
	}

	if filtered.Public != "public" {
		t.Errorf("Expected Public=public, got %s", filtered.Public)
	}
	if filtered.Skip != "" {
		t.Errorf("Expected Skip to be empty (filtered), got %s", filtered.Skip)
	}
}

// Benchmark tests
func BenchmarkApplyDefaults(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mock := &tagMock{}
		_ = ApplyDefaults(mock)
	}
}

func BenchmarkApplyDefaultsComplex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mock := &mockWithSlice{
			Apis: []Api{
				{Name: "api1", Url: "http://example.com"},
				{Name: "api2", Url: "http://example.org"},
			},
		}
		_ = ApplyDefaults(mock)
	}
}
