package validator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gv "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testUser struct {
	Name     string `json:"name"     validate:"required"`
	Email    string `json:"email"    validate:"required,email"`
	Age      int    `json:"age"      validate:"gte=18,lte=150"`
	Username string `json:"username" validate:"required,min=3,max=20"`
}

// ---- 构造 ----

func TestNew(t *testing.T) {
	v, err := New()
	require.NoError(t, err)
	assert.NotNil(t, v)
}

func TestNew_InvalidOptions(t *testing.T) {
	// 空 locales → defaultLocale 找不到 translator
	_, err := New(func(o *options) { o.locales = map[Locale]LocaleEntry{} })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in enabled locales")
}

func TestNew_DefaultLocaleNotInEnabled(t *testing.T) {
	// defaultLocale 设为一个不在 locales 中的值
	_, err := New(func(o *options) {
		delete(o.locales, LocaleZH)
		o.defaultLocale = LocaleZH
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in enabled locales")
}

func TestNew_InvalidCustomValidation(t *testing.T) {
	_, err := New(WithValidation("", func(fl gv.FieldLevel) bool { return true }))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Key cannot be empty")
}

func TestNew_NilCustomValidationFunc(t *testing.T) {
	_, err := New(WithValidation("foo", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "function cannot be empty")
}

func TestMustNew(t *testing.T) {
	assert.NotPanics(t, func() { MustNew() })
}

func TestMustNew_Panics(t *testing.T) {
	assert.Panics(t, func() { MustNew(func(o *options) { o.locales = map[Locale]LocaleEntry{} }) })
}

func TestDefault(t *testing.T) {
	v := Validate
	assert.NotNil(t, v)
	assert.Same(t, v, Validate) // 同一实例
}

func TestNewWithOptions(t *testing.T) {
	v, err := New(
		WithDefaultLocale(LocaleZH),
		WithFieldNameTag("json"),
	)
	require.NoError(t, err)
	assert.NotNil(t, v)
}

// ---- Struct ----

func TestStruct_Valid(t *testing.T) {
	v := MustNew()
	err := v.Struct(context.Background(), &testUser{
		Name:     "Alice",
		Email:    "alice@example.com",
		Age:      25,
		Username: "alice99",
	})
	assert.NoError(t, err)
}

func TestStruct_Invalid_ReturnsValidationError(t *testing.T) {
	v := MustNew()
	err := v.Struct(context.Background(), &testUser{
		Name:     "",    // required
		Email:    "bad", // email
		Age:      10,    // gte=18
		Username: "ab",  // min=3
	})
	require.Error(t, err)
	require.True(t, AsValidationError(err))
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Len(t, ve.Violations(), 4)
}

func TestStruct_ErrorMessage_NotEmpty(t *testing.T) {
	err := MustNew().Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)
	assert.NotEmpty(t, err.Error())
}

// ---- 字段名映射 ----

func TestFieldName_JSONTag(t *testing.T) {
	v := MustNew()
	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)

	require.True(t, AsValidationError(err))
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	for _, vi := range ve.Violations() {
		if vi.Tag == "required" {
			assert.Equal(t, "name", vi.Field, "预期 json tag 中的字段名")
			return
		}
	}
	t.Fatal("未找到 required violation")
}

func TestFieldName_GoStructField(t *testing.T) {
	v := MustNew(WithFieldNameTag(""))
	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)

	require.True(t, AsValidationError(err))
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	for _, vi := range ve.Violations() {
		if vi.Tag == "required" {
			assert.Equal(t, "Name", vi.Field, "预期 Go 结构体字段名")
			return
		}
	}
	t.Fatal("未找到 required violation")
}

// ---- Locale / 翻译 ----

func TestStruct_DefaultLocaleEN(t *testing.T) {
	v := MustNew()
	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestStruct_LocaleExtractorZH(t *testing.T) {
	v := MustNew(WithLocaleExtractor(func(_ context.Context) (Locale, bool) {
		return LocaleZH, true
	}))
	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "required")
}

func TestStruct_DefaultLocaleZH(t *testing.T) {
	v := MustNew(WithDefaultLocale(LocaleZH))
	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "required")
}

// ---- WithLocaleExtractor ----

type myLangKey struct{}

func TestLocaleExtractor_UsesContext(t *testing.T) {
	v := MustNew(WithLocaleExtractor(func(ctx context.Context) (Locale, bool) {
		if lang, ok := ctx.Value(myLangKey{}).(Locale); ok {
			return lang, true
		}
		return "", false
	}))

	ctx := context.WithValue(context.Background(), myLangKey{}, LocaleZH)
	err := v.Struct(ctx, &testUser{Name: ""})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "required")
}

func TestLocaleExtractor_FallbackToDefault(t *testing.T) {
	// localeExtractor 返回 false → 回退到 defaultLocale (EN)
	v := MustNew(WithLocaleExtractor(func(ctx context.Context) (Locale, bool) {
		return "", false
	}))

	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestLocaleExtractor_UnsupportedLocaleFallback(t *testing.T) {
	// localeExtractor 返回不在 locales 中的 locale → 退回 defaultLocale
	v := MustNew(WithLocaleExtractor(func(_ context.Context) (Locale, bool) {
		return Locale("fr"), true
	}))

	err := v.Struct(context.Background(), &testUser{Name: ""})
	require.Error(t, err)
	// 回退到默认 EN，应包含英文 required 消息
	assert.Contains(t, err.Error(), "required")
}

// ---- Var ----

func TestVar(t *testing.T) {
	v := MustNew()
	ctx := context.Background()
	assert.NoError(t, v.Var(ctx, "test@example.com", "email"))
	assert.Error(t, v.Var(ctx, "bad-email", "email"))
	assert.Error(t, v.Var(ctx, "", "required"))
	assert.NoError(t, v.Var(ctx, "hello", "required"))
}

func TestVar_WithLocale(t *testing.T) {
	v := MustNew(WithLocaleExtractor(func(_ context.Context) (Locale, bool) {
		return LocaleEN, true
	}))
	err := v.Var(context.Background(), "", "required")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

// ---- Violation 属性 ----

func TestViolation_Attributes(t *testing.T) {
	v := MustNew()
	err := v.Struct(context.Background(), &testUser{Name: "", Email: "bad"})
	require.Error(t, err)

	require.True(t, AsValidationError(err))
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	violations := ve.Violations()
	require.NotEmpty(t, violations)

	for _, vi := range violations {
		assert.NotEmpty(t, vi.Field)
		assert.NotEmpty(t, vi.Tag)
		assert.NotEmpty(t, vi.Message)
	}
}

// ---- 自定义校验（通过选项注册） ----

func TestWithValidation(t *testing.T) {
	v := MustNew(WithValidation("nonempty_str", func(fl gv.FieldLevel) bool {
		return fl.Field().String() != ""
	}))

	type payload struct {
		Val string `validate:"nonempty_str"`
	}
	ctx := context.Background()
	assert.NoError(t, v.Struct(ctx, &payload{Val: "ok"}))
	assert.Error(t, v.Struct(ctx, &payload{Val: ""}))
}

func TestWithStructValidation(t *testing.T) {
	type dateRange struct {
		Start int `validate:"required"`
		End   int `validate:"required"`
	}

	v := MustNew(WithStructValidation(func(sl gv.StructLevel) {
		dr := sl.Current().Interface().(dateRange)
		if dr.Start > dr.End {
			sl.ReportError(dr.End, "end", "End", "gtstart", "")
		}
	}, dateRange{}))

	ctx := context.Background()
	assert.NoError(t, v.Struct(ctx, &dateRange{Start: 1, End: 10}))
	assert.Error(t, v.Struct(ctx, &dateRange{Start: 10, End: 1}))
}

// ---- 错误工具 ----

func TestAsValidationError(t *testing.T) {
	v := MustNew()
	err := v.Struct(context.Background(), &testUser{Name: ""})
	assert.True(t, AsValidationError(err))
	assert.False(t, AsValidationError(nil))
	assert.False(t, AsValidationError(errors.New("plain")))
}

// ---- 并发安全 ----

func TestConcurrentValidation(t *testing.T) {
	v := MustNew()
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			u := &testUser{
				Name:     fmt.Sprintf("User%d", i),
				Email:    "user@example.com",
				Age:      25,
				Username: "user123",
			}
			assert.NoError(t, v.Struct(context.Background(), u))
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

// ---- 基准 ----

func BenchmarkStruct_Valid(b *testing.B) {
	v := MustNew()
	ctx := context.Background()
	u := &testUser{
		Name:     "Alice",
		Email:    "alice@example.com",
		Age:      25,
		Username: "alice99",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Struct(ctx, u)
	}
}

func BenchmarkStruct_Invalid(b *testing.B) {
	v := MustNew()
	ctx := context.Background()
	u := &testUser{Name: "", Email: "bad", Age: -1, Username: "ab"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Struct(ctx, u)
	}
}
