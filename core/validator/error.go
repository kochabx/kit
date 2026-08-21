package validator

import (
	"errors"
	"strings"
)

// Violation 描述单个字段的一条校验失败。
type Violation struct {
	// Field 是失败的字段名（受 WithFieldNameTag 影响）。
	Field string
	// Tag 是失败的校验约束标签（如 "required"）。
	Tag string
	// Param 是校验约束的参数值（如 gte=18 中的 "18"），无参数时为空字符串。
	Param string
	// Value 是校验时的字段值。
	Value any
	// Message 是经翻译的、人类可读的错误消息。
	Message string
}

// Error 是 Struct / Var 在有字段校验失败时返回的聚合错误。
//
// 推荐通过 errors.As 提取：
//
//	var ve *validator.Error
//	if errors.As(err, &ve) { … }
type Error struct {
	violations []Violation
	message    string
}

// Error 实现 error 接口。
func (ve *Error) Error() string { return ve.message }

// Violations 返回各字段校验错误，顺序与遇到的顺序一致。
func (ve *Error) Violations() []Violation { return ve.violations }

// newError 根据 Violation 切片构造 *Error。
func newError(violations []Violation) *Error {
	msgs := make([]string, len(violations))
	for i, v := range violations {
		msgs[i] = v.Message
	}
	return &Error{
		violations: violations,
		message:    strings.Join(msgs, "; "),
	}
}

// AsError 报告 err（或其 Unwrap 链路中的某个错误）是否为 *Error。
func AsError(err error) bool {
	var ve *Error
	return errors.As(err, &ve)
}
