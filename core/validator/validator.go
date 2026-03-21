package validator

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	gv "github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"
)

// Validate 是使用 JSON 字段名、英文为默认语言的包级 Validator，
// 可直接用于简单场景。
var Validate = New()

// validatorImpl 是 Validator 的具体实现。
type validatorImpl struct {
	v           *gv.Validate
	translators map[Lang]ut.Translator
	defaultLang Lang
}

// New 按选项创建一个新的 Validator。
// 零选项默认值：默认语言 en，启用 [en, zh]，字段名取自 json tag。
func New(opts ...Option) Validator {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}
	return build(cfg)
}

func build(cfg *config) *validatorImpl {
	v := gv.New()

	if cfg.fieldNameTag != "" {
		tag := cfg.fieldNameTag
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get(tag), ",", 2)[0]
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	}

	enLocale := en.New()
	zhLocale := zh.New()
	uni := ut.New(enLocale, enLocale, zhLocale)

	translators := make(map[Lang]ut.Translator, len(cfg.enabledLangs))
	for _, lang := range cfg.enabledLangs {
		switch lang {
		case LangEn:
			if trans, ok := uni.GetTranslator("en"); ok {
				translators[LangEn] = trans
				_ = en_translations.RegisterDefaultTranslations(v, trans)
			}
		case LangZh:
			if trans, ok := uni.GetTranslator("zh"); ok {
				translators[LangZh] = trans
				_ = zh_translations.RegisterDefaultTranslations(v, trans)
			}
		}
	}

	return &validatorImpl{
		v:           v,
		translators: translators,
		defaultLang: cfg.defaultLang,
	}
}

func (vi *validatorImpl) Struct(s any) error {
	return vi.StructCtx(context.Background(), s)
}

func (vi *validatorImpl) StructCtx(ctx context.Context, s any) error {
	if s == nil {
		return ErrNilTarget
	}
	return vi.wrap(ctx, vi.v.StructCtx(ctx, s))
}

func (vi *validatorImpl) Var(field any, tag string) error {
	return vi.VarCtx(context.Background(), field, tag)
}

func (vi *validatorImpl) VarCtx(ctx context.Context, field any, tag string) error {
	return vi.wrap(ctx, vi.v.VarCtx(ctx, field, tag))
}

func (vi *validatorImpl) RegisterValidation(tag string, fn gv.Func, callValidationEvenIfNull ...bool) error {
	return vi.v.RegisterValidation(tag, fn, callValidationEvenIfNull...)
}

func (vi *validatorImpl) RegisterStructValidation(fn func(gv.StructLevel), types ...any) {
	vi.v.RegisterStructValidation(fn, types...)
}

func (vi *validatorImpl) RegisterTagNameFunc(fn gv.TagNameFunc) {
	vi.v.RegisterTagNameFunc(fn)
}

// wrap 将底层 gv.ValidationErrors 转换为公共 ValidationErrors 类型。
func (vi *validatorImpl) wrap(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var verrs gv.ValidationErrors
	if !errors.As(err, &verrs) {
		return err
	}

	lang := vi.defaultLang
	if l, ok := ctx.Value(ctxLangKey{}).(Lang); ok {
		lang = l
	}

	trans := vi.translators[lang]
	if trans == nil {
		trans = vi.translators[vi.defaultLang]
	}

	fieldErrors := make([]FieldError, len(verrs))
	msgs := make([]string, len(verrs))
	for i, fe := range verrs {
		var msg string
		if trans != nil {
			msg = fe.Translate(trans)
		} else {
			msg = fe.Error()
		}
		fieldErrors[i] = &fieldError{
			field:   fe.Field(),
			tag:     fe.Tag(),
			value:   fe.Value(),
			message: msg,
		}
		msgs[i] = msg
	}

	return &validationErrors{
		fields:  fieldErrors,
		message: strings.Join(msgs, "; "),
	}
}
