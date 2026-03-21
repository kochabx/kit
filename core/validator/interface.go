package validator

import (
	"context"

	gv "github.com/go-playground/validator/v10"
)

// Lang 是用于错误消息翻译的语言代码。
type Lang string

const (
	LangEn Lang = "en"
	LangZh Lang = "zh"
)

type ctxLangKey struct{}

// ContextWithLang 返回携带指定语言的子 context，供 StructCtx / VarCtx 翻译错误消息时使用。
func ContextWithLang(ctx context.Context, lang Lang) context.Context {
	return context.WithValue(ctx, ctxLangKey{}, lang)
}

// Validator 是结构体 / 变量校验接口。
//
// 实现必须对并发调用安全。
// Register* 方法必须在首次校验前完成注册。
type Validator interface {
	// Struct 校验非 nil 结构体指针的所有导出字段，失败时返回 ValidationErrors。
	Struct(s any) error

	// StructCtx 同 Struct，但通过 ContextWithLang 获取语言进行翻译，未设置则退回默认语言。
	StructCtx(ctx context.Context, s any) error

	// Var 按 tag 表达式（如 "required,email"）校验单个值。
	Var(field any, tag string) error

	// VarCtx 同 Var，但通过 ctx 获取语言进行翻译。
	VarCtx(ctx context.Context, field any, tag string) error

	// RegisterValidation 为指定 tag 注册自定义校验函数。
	RegisterValidation(tag string, fn gv.Func, callValidationEvenIfNull ...bool) error

	// RegisterStructValidation 为一批结构体类型注册跨字段校验函数。
	RegisterStructValidation(fn func(gv.StructLevel), types ...any)

	// RegisterTagNameFunc 覆盖字段名提取逻辑（如使用 json tag 作为字段名）。
	RegisterTagNameFunc(fn gv.TagNameFunc)
}

// ValidationErrors 是 Struct / StructCtx 在有字段校验失败时返回的聚合错误。
//
// 推荐通过 errors.As 提取：
//
//	var ve ValidationErrors
//	if errors.As(err, &ve) { … }
type ValidationErrors interface {
	error
	// Fields 返回各字段校验错误，顺序与遇到的顺序一致。
	Fields() []FieldError
}

// FieldError 描述单个字段的一条校验失败。
type FieldError interface {
	// Field 返回字段名（受 RegisterTagNameFunc 影响）。
	Field() string
	// Tag 返回失败的校验约束标签（如 "required"）。
	Tag() string
	// Value 返回校验时的字段值。
	Value() any
	// Message 返回经翻译的、人类可读的错误消息。
	Message() string
}
