package validator

import "errors"

// ErrNilTarget 当向 Struct / StructCtx 传入 nil 时返回此错误。
var ErrNilTarget = errors.New("validator: validation target must not be nil")

// validationErrors 实现 ValidationErrors。
type validationErrors struct {
	fields  []FieldError
	message string
}

func (ve *validationErrors) Error() string        { return ve.message }
func (ve *validationErrors) Fields() []FieldError { return ve.fields }

// fieldError 实现 FieldError。
type fieldError struct {
	field   string
	tag     string
	value   any
	message string
}

func (fe *fieldError) Field() string   { return fe.field }
func (fe *fieldError) Tag() string     { return fe.tag }
func (fe *fieldError) Value() any      { return fe.value }
func (fe *fieldError) Message() string { return fe.message }

// IsValidationError 报告 err（或其链路中的某个错误）是否为 ValidationErrors。
func IsValidationError(err error) bool {
	var ve ValidationErrors
	return errors.As(err, &ve)
}

// FieldErrors 从 err 中提取 []FieldError；若 err 不是 ValidationErrors 则返回 nil。
func FieldErrors(err error) []FieldError {
	var ve ValidationErrors
	if errors.As(err, &ve) {
		return ve.Fields()
	}
	return nil
}
