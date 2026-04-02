package validator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/locales"
	ut "github.com/go-playground/universal-translator"
	gv "github.com/go-playground/validator/v10"
)

// Validator 是结构体 / 变量校验接口。
type Validator interface {
	// Struct 校验非 nil 结构体指针的所有导出字段，失败时返回 *ValidationError。
	// 通过 WithLocaleExtractor 设置的 Locale 用于翻译错误消息。
	Struct(ctx context.Context, s any) error

	// Var 按 tag 表达式（如 "required,email"）校验单个值。
	Var(ctx context.Context, field any, tag string) error
}

// validator 是 Validator 的具体实现。
type validator struct {
	v               *gv.Validate
	translators     map[Locale]ut.Translator
	defaultLocale   Locale
	localeExtractor func(context.Context) (Locale, bool)
}

// New 按选项创建一个新的 Validator。
// 零选项默认值：默认语言 en，启用 [en, zh]，字段名取自 json tag。
func New(opts ...Option) (Validator, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(o)
	}
	return build(o)
}

// MustNew 同 New，但在出错时 panic。
func MustNew(opts ...Option) Validator {
	v, err := New(opts...)
	if err != nil {
		panic(fmt.Sprintf("validator: %v", err))
	}
	return v
}

var Validate = MustNew()

func build(o *options) (*validator, error) {
	v := gv.New()

	if o.fieldNameTag != "" {
		tag := o.fieldNameTag
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get(tag), ",", 2)[0]
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	}

	// 收集所有 locale 实例，构建 UniversalTranslator
	defaultEntry, ok := o.locales[o.defaultLocale]
	if !ok {
		return nil, fmt.Errorf("validator: default locale %q not in enabled locales", o.defaultLocale)
	}
	allLocs := make([]locales.Translator, 0, len(o.locales))
	for _, entry := range o.locales {
		allLocs = append(allLocs, entry.Loc)
	}
	uni := ut.New(defaultEntry.Loc, allLocs...)

	translators := make(map[Locale]ut.Translator, len(o.locales))
	for locale, entry := range o.locales {
		trans, found := uni.GetTranslator(entry.Loc.Locale())
		if !found {
			return nil, fmt.Errorf("validator: translator not found for locale %q", locale)
		}
		if err := entry.Register(v, trans); err != nil {
			return nil, fmt.Errorf("validator: register %s translations: %w", locale, err)
		}
		translators[locale] = trans
	}

	for _, cv := range o.validations {
		if err := v.RegisterValidation(cv.tag, cv.fn, cv.callEvenIfNull); err != nil {
			return nil, fmt.Errorf("validator: register validation %q: %w", cv.tag, err)
		}
	}

	for _, sv := range o.structValidations {
		v.RegisterStructValidation(sv.fn, sv.types...)
	}

	return &validator{
		v:               v,
		translators:     translators,
		defaultLocale:   o.defaultLocale,
		localeExtractor: o.localeExtractor,
	}, nil
}

func (vi *validator) Struct(ctx context.Context, s any) error {
	return vi.wrap(ctx, vi.v.StructCtx(ctx, s))
}

func (vi *validator) Var(ctx context.Context, field any, tag string) error {
	return vi.wrap(ctx, vi.v.VarCtx(ctx, field, tag))
}

// resolveLocale 按优先级解析 Locale：localeExtractor → defaultLocale。
func (vi *validator) resolveLocale(ctx context.Context) Locale {
	if vi.localeExtractor != nil {
		if l, ok := vi.localeExtractor(ctx); ok {
			return l
		}
	}
	return vi.defaultLocale
}

// wrap 将底层 gv.ValidationErrors 转换为 *ValidationError。
func (vi *validator) wrap(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var verrs gv.ValidationErrors
	if !errors.As(err, &verrs) {
		return err
	}

	locale := vi.resolveLocale(ctx)

	trans := vi.translators[locale]
	if trans == nil {
		trans = vi.translators[vi.defaultLocale]
	}

	violations := make([]Violation, len(verrs))
	for i, fe := range verrs {
		var msg string
		if trans != nil {
			msg = fe.Translate(trans)
		} else {
			msg = fe.Error()
		}
		violations[i] = Violation{
			Field:   fe.Field(),
			Tag:     fe.Tag(),
			Value:   fe.Value(),
			Message: msg,
		}
	}

	return newValidationError(violations)
}
