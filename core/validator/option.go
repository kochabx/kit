package validator

import (
	"context"

	"github.com/go-playground/locales"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	gv "github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"
)

type Option func(*options)

type validation struct {
	tag            string
	fn             gv.Func
	callEvenIfNull bool
}

type structValidation struct {
	fn    func(gv.StructLevel)
	types []any
}

// LocaleEntry 描述一种语言的翻译器实例及其注册函数。
type LocaleEntry struct {
	Loc      locales.Translator
	Register func(*gv.Validate, ut.Translator) error
}

type options struct {
	fieldNameTag      string
	defaultLocale     Locale
	locales           map[Locale]LocaleEntry
	localeExtractor   func(context.Context) (Locale, bool)
	validations       []validation
	structValidations []structValidation
}

func defaultOptions() *options {
	return &options{
		fieldNameTag:  "json",
		defaultLocale: LocaleEN,
		locales: map[Locale]LocaleEntry{
			LocaleEN: {Loc: en.New(), Register: en_translations.RegisterDefaultTranslations},
			LocaleZH: {Loc: zh.New(), Register: zh_translations.RegisterDefaultTranslations},
		},
	}
}

// WithDefaultLocale 设置当 localeExtractor 未命中时所用的回退语言。
// 默认值：LocaleEN。
func WithDefaultLocale(locale Locale) Option {
	return func(o *options) {
		o.defaultLocale = locale
	}
}

// WithLocale 注册一个语言的翻译。
// loc 为 go-playground/locales 中的 Translator 实例，
// register 用于向 validator 注册该语言的默认翻译。
func WithLocale(locale Locale, entry LocaleEntry) Option {
	return func(o *options) {
		o.locales[locale] = entry
	}
}

// WithFieldNameTag 指定用于提取字段名的结构体 tag（例如 "json"、"yaml"）。
// tag 值的首个逗号前部分将作为字段名出现在错误消息中。
// 传入空字符串则使用 Go 结构体字段名。
// 默认值："json"。
func WithFieldNameTag(tag string) Option {
	return func(o *options) {
		o.fieldNameTag = tag
	}
}

// WithLocaleExtractor 设置自定义的 Locale 提取函数，用于从 context 中获取语言。
// 典型场景：HTTP 中间件已将 Accept-Language 解析结果存入 context。
//
// Locale 解析优先级：localeExtractor → defaultLocale。
func WithLocaleExtractor(fn func(context.Context) (Locale, bool)) Option {
	return func(o *options) {
		o.localeExtractor = fn
	}
}

// WithValidation 注册自定义校验函数
// tag 为校验标签名，fn 为校验函数。
// callEvenIfNull 可选，为 true 时即使字段值为零值也会调用校验函数。
func WithValidation(tag string, fn gv.Func, callEvenIfNull ...bool) Option {
	return func(o *options) {
		cv := validation{tag: tag, fn: fn}
		if len(callEvenIfNull) > 0 {
			cv.callEvenIfNull = callEvenIfNull[0]
		}
		o.validations = append(o.validations, cv)
	}
}

// WithStructValidation 注册跨字段的结构体级校验函数
func WithStructValidation(fn func(gv.StructLevel), types ...any) Option {
	return func(o *options) {
		o.structValidations = append(o.structValidations, structValidation{fn: fn, types: types})
	}
}
