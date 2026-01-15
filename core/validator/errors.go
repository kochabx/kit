package validator

import (
	"fmt"
	"strings"

	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
)

// validationErrorsImpl 校验错误实现
type validationErrorsImpl struct {
	fieldErrors []FieldError
	message     string
}

// Error 返回错误信息
func (ve *validationErrorsImpl) Error() string {
	return ve.message
}

// Errors 返回错误列表
func (ve *validationErrorsImpl) Errors() []FieldError {
	return ve.fieldErrors
}

// HasErrors 是否有错误
func (ve *validationErrorsImpl) HasErrors() bool {
	return len(ve.fieldErrors) > 0
}

// fieldErrorImpl 字段错误实现
type fieldErrorImpl struct {
	fieldError  validator.FieldError
	message     string
	translators map[string]ut.Translator
}

// Field 字段名
func (fe *fieldErrorImpl) Field() string {
	return fe.fieldError.Field()
}

// Tag 校验标签
func (fe *fieldErrorImpl) Tag() string {
	return fe.fieldError.Tag()
}

// Value 字段值
func (fe *fieldErrorImpl) Value() any {
	return fe.fieldError.Value()
}

// Message 错误消息
func (fe *fieldErrorImpl) Message() string {
	return fe.message
}

// Translate 翻译错误消息
func (fe *fieldErrorImpl) Translate(lang string) string {
	if trans, exists := fe.translators[lang]; exists {
		return fe.fieldError.Translate(trans)
	}
	return fe.message
}

// ValidationResult 校验结果
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// ValidationError 校验错误详情
type ValidationError struct {
	Field   string `json:"field"`
	Tag     string `json:"tag"`
	Value   any    `json:"value"`
	Message string `json:"message"`
}

// ToValidationResult 将错误转换为校验结果
func ToValidationResult(err error) *ValidationResult {
	if err == nil {
		return &ValidationResult{Valid: true}
	}

	result := &ValidationResult{Valid: false}

	if validationErr, ok := err.(ValidationErrors); ok {
		for _, fieldErr := range validationErr.Errors() {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fieldErr.Field(),
				Tag:     fieldErr.Tag(),
				Value:   fieldErr.Value(),
				Message: fieldErr.Message(),
			})
		}
	}

	return result
}

// ErrorsToString 将错误列表转换为字符串
func ErrorsToString(errors []FieldError, separator string) string {
	if len(errors) == 0 {
		return ""
	}

	if separator == "" {
		separator = "; "
	}

	var messages []string
	for _, err := range errors {
		messages = append(messages, err.Message())
	}

	return strings.Join(messages, separator)
}

// GetFieldErrorMessage 获取指定字段的错误消息
func GetFieldErrorMessage(err error, field string) string {
	if validationErr, ok := err.(ValidationErrors); ok {
		for _, fieldErr := range validationErr.Errors() {
			if fieldErr.Field() == field {
				return fieldErr.Message()
			}
		}
	}
	return ""
}

// HasFieldError 检查是否存在指定字段的错误
func HasFieldError(err error, field string) bool {
	if validationErr, ok := err.(ValidationErrors); ok {
		for _, fieldErr := range validationErr.Errors() {
			if fieldErr.Field() == field {
				return true
			}
		}
	}
	return false
}

// GetFieldErrorsByTag 根据标签获取字段错误
func GetFieldErrorsByTag(err error, tag string) []FieldError {
	var result []FieldError

	if validationErr, ok := err.(ValidationErrors); ok {
		for _, fieldErr := range validationErr.Errors() {
			if fieldErr.Tag() == tag {
				result = append(result, fieldErr)
			}
		}
	}

	return result
}

// FormatError 格式化错误信息
func FormatError(err error, format string) string {
	if err == nil {
		return ""
	}

	if validationErr, ok := err.(ValidationErrors); ok {
		var formatted []string
		for _, fieldErr := range validationErr.Errors() {
			msg := fmt.Sprintf(format,
				fieldErr.Field(),
				fieldErr.Tag(),
				fieldErr.Value(),
				fieldErr.Message())
			formatted = append(formatted, msg)
		}
		return strings.Join(formatted, "; ")
	}

	return err.Error()
}

// IsValidationError 检查是否为校验错误
func IsValidationError(err error) bool {
	_, ok := err.(ValidationErrors)
	return ok
}
