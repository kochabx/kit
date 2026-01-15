package validator

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"
)

// validatorImpl 校验器实现
type validatorImpl struct {
	validator    *validator.Validate
	uni          *ut.UniversalTranslator
	translators  map[string]ut.Translator
	mutex        sync.RWMutex
	enabledLangs []string
	defaultLang  string
}

// Validate 全局校验器实例
var (
	Validate Validator
	once     sync.Once
)

func init() {
	once.Do(func() {
		Validate = New()
	})
}

// New 创建新的校验器实例
func New(opts ...ValidationOption) Validator {
	v := &validatorImpl{
		validator:    validator.New(),
		translators:  make(map[string]ut.Translator),
		enabledLangs: []string{"en", "zh"},
		defaultLang:  "en",
	}

	// 初始化通用翻译器
	enLocale := en.New()
	zhLocale := zh.New()
	v.uni = ut.New(enLocale, enLocale, zhLocale)

	// 应用选项
	for _, opt := range opts {
		opt(v)
	}

	// 初始化翻译器
	v.initTranslators()

	return v
}

// initTranslators 初始化翻译器
func (v *validatorImpl) initTranslators() {
	for _, lang := range v.enabledLangs {
		switch lang {
		case "en":
			if trans, found := v.uni.GetTranslator("en"); found {
				v.translators["en"] = trans
				// 注册英文翻译
				_ = en_translations.RegisterDefaultTranslations(v.validator, trans)
			}
		case "zh":
			if trans, found := v.uni.GetTranslator("zh"); found {
				v.translators["zh"] = trans
				// 注册中文翻译
				_ = zh_translations.RegisterDefaultTranslations(v.validator, trans)
			}
		}
	}
}

// Struct 校验结构体
func (v *validatorImpl) Struct(s any) error {
	if s == nil {
		return errors.New("validation target cannot be nil")
	}

	err := v.validator.Struct(s)
	if err != nil {
		return v.translateError(err, v.defaultLang)
	}
	return nil
}

// StructCtx 带上下文校验结构体
func (v *validatorImpl) StructCtx(ctx context.Context, s any) error {
	if s == nil {
		return errors.New("validation target cannot be nil")
	}

	err := v.validator.StructCtx(ctx, s)
	if err != nil {
		return v.translateError(err, v.defaultLang)
	}
	return nil
}

// GetValidator 获取底层的validator实例
func (v *validatorImpl) GetValidator() *validator.Validate {
	return v.validator
}

// translateError 翻译错误
func (v *validatorImpl) translateError(err error, lang string) error {
	if err == nil {
		return nil
	}

	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return err
	}

	v.mutex.RLock()
	trans, exists := v.translators[lang]
	v.mutex.RUnlock()

	if !exists {
		// 如果指定语言的翻译器不存在，使用默认语言
		v.mutex.RLock()
		trans, exists = v.translators[v.defaultLang]
		v.mutex.RUnlock()
		if !exists {
			return err
		}
	}

	var fieldErrors []FieldError
	var errorMessages []string

	for _, fe := range validationErrors {
		fieldError := &fieldErrorImpl{
			fieldError:  fe,
			message:     fe.Translate(trans),
			translators: v.translators,
		}
		fieldErrors = append(fieldErrors, fieldError)
		errorMessages = append(errorMessages, fieldError.Message())
	}

	return &validationErrorsImpl{
		fieldErrors: fieldErrors,
		message:     strings.Join(errorMessages, "; "),
	}
}
