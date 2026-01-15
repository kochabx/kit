package validator

import (
	"context"

	"github.com/go-playground/validator/v10"
)

// Validator 定义校验器接口
type Validator interface {
	// Struct 校验结构体
	Struct(s any) error

	// StructCtx 带上下文校验结构体
	StructCtx(ctx context.Context, s any) error

	// GetValidator 获取底层的validator实例
	GetValidator() *validator.Validate
}

// ValidationErrors 校验错误接口
type ValidationErrors interface {
	error
	// Error 返回错误信息
	Error() string
	// Errors 返回错误列表
	Errors() []FieldError
	// HasErrors 是否有错误
	HasErrors() bool
}

// FieldError 字段错误接口
type FieldError interface {
	// Field 字段名
	Field() string
	// Tag 校验标签
	Tag() string
	// Value 字段值
	Value() any
	// Message 错误消息
	Message() string
	// Translate 翻译错误消息
	Translate(lang string) string
}

// ValidationOption 校验器选项
type ValidationOption func(*validatorImpl)

// WithTagName 设置校验标签名
func WithTagName(tagName string) ValidationOption {
	return func(v *validatorImpl) {
		v.validator.SetTagName(tagName)
	}
}

// WithTranslator 设置翻译器语言
func WithTranslator(langs ...string) ValidationOption {
	return func(v *validatorImpl) {
		v.enabledLangs = append(v.enabledLangs, langs...)
	}
}
