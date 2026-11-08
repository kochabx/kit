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

// Validator validates structs and single values. Each call requires a non-nil Context.
// Instances returned by New can be shared; callers manage custom callbacks and concurrent access to target data.
type Validator interface {
	// Struct validates a struct value or a non-nil struct pointer, returning *Error for field failures.
	// Invalid targets return the underlying *gv.InvalidValidationError.
	Struct(ctx context.Context, s any) error

	// Var validates one value against a tag expression such as "required,email", returning *Error on failure.
	// Invalid or unregistered validation tags retain the underlying panic behavior.
	Var(ctx context.Context, field any, tag string) error
}

// validator registers rules and translations during construction and does not change configuration during validation.
type validator struct {
	v               *gv.Validate
	translators     map[Locale]ut.Translator
	defaultLocale   Locale
	localeExtractor func(context.Context) (Locale, bool)
}

// New creates a validator with English and Simplified Chinese translations, English fallback, and json field names.
// Invalid configuration or a registration panic returns an error wrapping ErrInvalidOption.
// Do not mutate the underlying validator or translators through references retained by registration callbacks.
func New(opts ...Option) (result Validator, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("%w: configuration callback or registration panicked", ErrInvalidOption)
		}
	}()
	o := defaultOptions()
	for index, fn := range opts {
		if fn == nil {
			return nil, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		fn(o)
	}
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	built, err := build(o)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidOption, err)
	}
	return built, nil
}

// Validate is the shared instance created with default options; initialization failure panics immediately.
// Do not replace it at runtime. Use New for custom configuration.
var Validate = mustDefault()

func mustDefault() Validator {
	v, err := New()
	if err != nil {
		panic(err)
	}
	return v
}

func build(o *options) (*validator, error) {
	v := gv.New()

	if o.fieldNameTag != "" {
		tag := o.fieldNameTag
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name, _, _ := strings.Cut(fld.Tag.Get(tag), ",")
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	}

	// Map application locales to translator instances, then register translations by translator identifier.
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
		var err error
		if cv.ctxFn != nil {
			err = v.RegisterValidationCtx(cv.tag, cv.ctxFn, cv.callEvenIfNull)
		} else {
			err = v.RegisterValidation(cv.tag, cv.fn, cv.callEvenIfNull)
		}
		if err != nil {
			return nil, fmt.Errorf("validator: register validation %q: %w", cv.tag, err)
		}
	}

	for _, sv := range o.structValidations {
		if sv.ctxFn != nil {
			v.RegisterStructValidationCtx(sv.ctxFn, sv.types...)
		} else {
			v.RegisterStructValidation(sv.fn, sv.types...)
		}
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

// resolveLocale prefers a locale from the extractor and otherwise returns the default.
// wrap handles unsupported locales when looking up translators.
func (vi *validator) resolveLocale(ctx context.Context) Locale {
	if vi.localeExtractor != nil {
		if l, ok := vi.localeExtractor(ctx); ok {
			return l
		}
	}
	return vi.defaultLocale
}

// wrap translates field failures and preserves their cause; other errors pass through unchanged.
func (vi *validator) wrap(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	verrs, ok := errors.AsType[gv.ValidationErrors](err)
	if !ok {
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
			Param:   fe.Param(),
			Path:    fe.Namespace(),
			Message: msg,
		}
	}

	return newError(violations, err)
}
