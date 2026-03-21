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
	assert.NotNil(t, New())
}

func TestValidate(t *testing.T) {
	assert.NotNil(t, Validate)
}

func TestNewWithOptions(t *testing.T) {
	v := New(
		WithDefaultLang(LangZh),
		WithLangs(LangEn, LangZh),
		WithFieldNameTag("json"),
	)
	assert.NotNil(t, v)
}

// ---- Struct ----

func TestStruct_Valid(t *testing.T) {
	v := New()
	err := v.Struct(&testUser{
		Name:     "Alice",
		Email:    "alice@example.com",
		Age:      25,
		Username: "alice99",
	})
	assert.NoError(t, err)
}

func TestStruct_Nil(t *testing.T) {
	err := New().Struct(nil)
	assert.ErrorIs(t, err, ErrNilTarget)
}

func TestStruct_Invalid_ReturnsValidationErrors(t *testing.T) {
	v := New()
	err := v.Struct(&testUser{
		Name:     "",    // required
		Email:    "bad", // email
		Age:      10,    // gte=18
		Username: "ab",  // min=3
	})
	require.Error(t, err)
	assert.True(t, IsValidationError(err))

	var ve ValidationErrors
	require.True(t, errors.As(err, &ve))
	assert.Len(t, ve.Fields(), 4)
}

func TestStruct_ErrorMessage_NotEmpty(t *testing.T) {
	err := New().Struct(&testUser{Name: ""})
	require.Error(t, err)
	assert.NotEmpty(t, err.Error())
}

// ---- 字段名映射 ----

func TestFieldName_JSONTag(t *testing.T) {
	// 默认使用 json tag，字段名应为小写
	v := New()
	err := v.Struct(&testUser{Name: ""})
	require.Error(t, err)

	for _, fe := range FieldErrors(err) {
		if fe.Tag() == "required" {
			assert.Equal(t, "name", fe.Field(), "预期 json tag 中的字段名")
			break
		}
	}
}

func TestFieldName_GoStructField(t *testing.T) {
	// 不使用任何 tag 时，使用 Go 结构体字段名
	v := New(WithFieldNameTag(""))
	err := v.Struct(&testUser{Name: ""})
	require.Error(t, err)

	for _, fe := range FieldErrors(err) {
		if fe.Tag() == "required" {
			assert.Equal(t, "Name", fe.Field(), "预期 Go 结构体字段名")
			break
		}
	}
}

// ---- 语言 / 翻译 ----

func TestStructCtx_DefaultLangEn(t *testing.T) {
	v := New()
	err := v.Struct(&testUser{Name: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestStructCtx_LangZh(t *testing.T) {
	v := New()
	ctx := ContextWithLang(context.Background(), LangZh)
	err := v.StructCtx(ctx, &testUser{Name: ""})
	require.Error(t, err)
	// zh 翻译结果与 en 不同（不含 "required" 英文单词）
	assert.NotContains(t, err.Error(), "required")
}

func TestStructCtx_DefaultLangZh(t *testing.T) {
	v := New(WithDefaultLang(LangZh))
	err := v.Struct(&testUser{Name: ""})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "required")
}

// ---- Var / VarCtx ----

func TestVar(t *testing.T) {
	v := New()
	assert.NoError(t, v.Var("test@example.com", "email"))
	assert.Error(t, v.Var("bad-email", "email"))
	assert.Error(t, v.Var("", "required"))
	assert.NoError(t, v.Var("hello", "required"))
}

func TestVarCtx(t *testing.T) {
	v := New()
	ctx := ContextWithLang(context.Background(), LangEn)
	err := v.VarCtx(ctx, "", "required")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

// ---- FieldError 属性 ----

func TestFieldError_Attributes(t *testing.T) {
	v := New()
	err := v.Struct(&testUser{Name: "", Email: "bad"})
	require.Error(t, err)

	fes := FieldErrors(err)
	require.NotEmpty(t, fes)

	for _, fe := range fes {
		assert.NotEmpty(t, fe.Field())
		assert.NotEmpty(t, fe.Tag())
		assert.NotEmpty(t, fe.Message())
	}
}

// ---- 自定义校验 ----

func TestRegisterValidation(t *testing.T) {
	v := New()
	err := v.RegisterValidation("nonempty_str", func(fl gv.FieldLevel) bool {
		return fl.Field().String() != ""
	})
	require.NoError(t, err)

	type payload struct {
		Val string `validate:"nonempty_str"`
	}
	assert.NoError(t, v.Struct(&payload{Val: "ok"}))
	assert.Error(t, v.Struct(&payload{Val: ""}))
}

func TestRegisterStructValidation(t *testing.T) {
	type dateRange struct {
		Start int `validate:"required"`
		End   int `validate:"required"`
	}

	v := New()
	v.RegisterStructValidation(func(sl gv.StructLevel) {
		dr := sl.Current().Interface().(dateRange)
		if dr.Start > dr.End {
			sl.ReportError(dr.End, "end", "End", "gtstart", "")
		}
	}, dateRange{})

	assert.NoError(t, v.Struct(&dateRange{Start: 1, End: 10}))
	assert.Error(t, v.Struct(&dateRange{Start: 10, End: 1}))
}

// ---- 错误工具 ----

func TestIsValidationError(t *testing.T) {
	v := New()
	err := v.Struct(&testUser{Name: ""})
	assert.True(t, IsValidationError(err))
	assert.False(t, IsValidationError(errors.New("plain error")))
	assert.False(t, IsValidationError(nil))
}

func TestFieldErrors_Helper(t *testing.T) {
	v := New()
	err := v.Struct(&testUser{Name: ""})
	fes := FieldErrors(err)
	assert.NotEmpty(t, fes)

	assert.Nil(t, FieldErrors(nil))
	assert.Nil(t, FieldErrors(errors.New("plain")))
}

// ---- 并发安全 ----

func TestConcurrentValidation(t *testing.T) {
	v := New()
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
			assert.NoError(t, v.Struct(u))
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

// ---- 基准 ----

func BenchmarkStruct_Valid(b *testing.B) {
	v := New()
	u := &testUser{
		Name:     "Alice",
		Email:    "alice@example.com",
		Age:      25,
		Username: "alice99",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Struct(u)
	}
}

func BenchmarkStruct_Invalid(b *testing.B) {
	v := New()
	u := &testUser{Name: "", Email: "bad", Age: -1, Username: "ab"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Struct(u)
	}
}
