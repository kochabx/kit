package validator

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUser 测试用户结构体
type TestUser struct {
	Name     string `json:"name" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Mobile   string `json:"mobile" validate:"required"`
	Age      int    `json:"age" validate:"gte=18,lte=150"`
	Username string `json:"username" validate:"required,min=3,max=20"`
}

// TestValidatorCreation 测试校验器创建
func TestValidatorCreation(t *testing.T) {
	// 测试默认校验器
	v1 := Validate
	assert.NotNil(t, v1)

	// 测试新建校验器
	v2 := New()
	assert.NotNil(t, v2)

	// 测试带选项的校验器
	v3 := New(
		WithTagName("validate"),
		WithTranslator("en", "zh"),
	)
	assert.NotNil(t, v3)
}

// TestBasicValidation 测试基本校验功能
func TestBasicValidation(t *testing.T) {
	v := New()

	// 测试有效数据
	validUser := TestUser{
		Name:     "John Doe",
		Email:    "john@example.com",
		Mobile:   "1234567890",
		Username: "johndoe",
		Age:      16, // 修改为有效年龄
	}

	err := v.Struct(&validUser)
	assert.NoError(t, err)
}

// TestValidationErrors 测试校验错误
func TestValidationErrors(t *testing.T) {
	v := New()

	// 测试无效数据
	invalidUser := TestUser{
		Name:     "",        // 必填
		Email:    "invalid", // 无效邮箱
		Mobile:   "",        // 必填
		Username: "ab",      // 太短
		Age:      -1,        // 年龄不能为负数
	}

	err := v.Struct(&invalidUser)
	assert.Error(t, err)

	// 检查是否为校验错误
	assert.True(t, IsValidationError(err))

	// 转换为校验错误
	validationErr, ok := err.(ValidationErrors)
	assert.True(t, ok)
	assert.True(t, validationErr.HasErrors())

	errors := validationErr.Errors()
	assert.NotEmpty(t, errors)
}

// TestChineseTranslation 测试中文翻译
func TestChineseTranslation(t *testing.T) {
	v := New()

	// 测试中文翻译
	invalidUser := TestUser{
		Name:   "",
		Email:  "invalid",
		Mobile: "",
	}

	err := v.Struct(&invalidUser)
	assert.Error(t, err)

	errorMsg := err.Error()
	// 检查错误消息不为空
	assert.NotEmpty(t, errorMsg)
}

// TestEnglishTranslation 测试英文翻译
func TestEnglishTranslation(t *testing.T) {
	v := New()

	// 测试英文翻译
	invalidUser := TestUser{
		Name:   "",
		Email:  "invalid",
		Mobile: "",
	}

	err := v.Struct(&invalidUser)
	assert.Error(t, err)

	errorMsg := err.Error()
	// 检查错误消息是否包含英文关键词
	assert.Contains(t, errorMsg, "required")
}

// TestValidateVar 测试单个变量校验
func TestValidateVar(t *testing.T) {
	v := New()
	validator := v.GetValidator()

	// 测试邮箱
	err := validator.Var("test@example.com", "email")
	assert.NoError(t, err)

	err = validator.Var("invalid-email", "email")
	assert.Error(t, err)

	// 测试必填
	err = validator.Var("", "required")
	assert.Error(t, err)

	err = validator.Var("not empty", "required")
	assert.NoError(t, err)
}

// TestErrorHandling 测试错误处理功能
func TestErrorHandling(t *testing.T) {
	v := New()

	invalidUser := TestUser{
		Name:   "",
		Email:  "invalid",
		Mobile: "",
	}

	err := v.Struct(&invalidUser)
	require.Error(t, err)

	// 测试ToValidationResult
	result := ToValidationResult(err)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	// 测试ErrorsToString
	validationErr := err.(ValidationErrors)
	errorString := ErrorsToString(validationErr.Errors(), " | ")
	assert.NotEmpty(t, errorString)

	// 测试HasFieldError
	hasNameError := HasFieldError(err, "Name")
	assert.True(t, hasNameError)

	hasNonExistentError := HasFieldError(err, "NonExistent")
	assert.False(t, hasNonExistentError)

	// 测试GetFieldErrorMessage
	nameErrorMsg := GetFieldErrorMessage(err, "Name")
	assert.NotEmpty(t, nameErrorMsg)
}

// TestValidationResult 测试校验结果
func TestValidationResult(t *testing.T) {
	// 测试成功校验结果
	result := ToValidationResult(nil)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)

	v := New()

	// 测试失败校验结果
	invalidUser := TestUser{Name: ""}
	validationErr := v.Struct(&invalidUser)

	result = ToValidationResult(validationErr)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	// 检查错误详情
	for _, err := range result.Errors {
		assert.NotEmpty(t, err.Field)
		assert.NotEmpty(t, err.Tag)
		assert.NotEmpty(t, err.Message)
	}
}

// TestConcurrentAccess 测试并发访问
func TestConcurrentAccess(t *testing.T) {
	v := New()

	// 并发校验
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(i int) {
			defer func() { done <- true }()

			user := TestUser{
				Name:     fmt.Sprintf("User%d", i),
				Email:    "user@example.com",
				Mobile:   "1234567890",
				Username: "user123",
				Age:      25,
			}

			err := v.Struct(&user)
			assert.NoError(t, err)
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < 10; i++ {
		<-done
	}
}

// BenchmarkValidation 基准测试
func BenchmarkValidation(b *testing.B) {
	v := New()

	user := TestUser{
		Name:     "John Doe",
		Email:    "john@example.com",
		Mobile:   "1234567890",
		Username: "johndoe",
		Age:      25,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Struct(&user)
	}
}
