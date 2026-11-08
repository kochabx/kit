package validator

import (
	"context"
	"slices"

	"github.com/go-playground/locales"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	gv "github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"
)

// Option configures New. Configuration is fixed after construction.
type Option func(*options)

type validation struct {
	tag            string
	fn             gv.Func
	ctxFn          gv.FuncCtx
	callEvenIfNull bool
}

type structValidation struct {
	fn    func(gv.StructLevel)
	ctxFn gv.StructLevelFuncCtx
	types []any
}

// LocaleEntry pairs a locale with its translation registrar; both must be non-nil.
type LocaleEntry struct {
	// Loc supplies locale information. Distinct Locale keys must not share the same Loc.Locale() identifier.
	Loc locales.Translator
	// Register runs only during New to register this locale's translations.
	// Do not retain and mutate the supplied validator or translator during validation.
	Register func(*gv.Validate, ut.Translator) error
}

type options struct {
	invalidArity      bool
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

// WithDefaultLocale sets the fallback locale, defaulting to LocaleEN.
// The locale must be enabled. It is used when extraction fails or returns an unsupported locale.
func WithDefaultLocale(locale Locale) Option {
	return func(o *options) {
		o.defaultLocale = locale
	}
}

// WithLocale registers or replaces a locale. LocaleEN and LocaleZH are enabled by default.
// entry must provide both a locale instance and a translation registrar.
func WithLocale(locale Locale, entry LocaleEntry) Option {
	return func(o *options) {
		o.locales[locale] = entry
	}
}

// WithFieldNameTag selects the struct tag used for field names, such as "json" or "yaml".
// The tag value before the first comma becomes the field name in validation messages.
// Missing, empty, or "-" tag names fall back to the Go field name without skipping validation.
// An empty tag argument always selects Go field names.
// The default tag is "json".
func WithFieldNameTag(tag string) Option {
	return func(o *options) {
		o.fieldNameTag = tag
	}
}

// WithLocaleExtractor sets the callback that extracts a Locale from context.
// For example, HTTP middleware may store a parsed Accept-Language value in context.
//
// False or an unsupported locale selects the default locale; nil disables extraction.
// fn must be safe for concurrent calls.
func WithLocaleExtractor(fn func(context.Context) (Locale, bool)) Option {
	return func(o *options) {
		o.localeExtractor = fn
	}
}

// WithValidation registers a field validator with a nonempty, nonreserved, unique custom tag.
// fn must be non-nil; New returns an error for invalid registrations.
// callEvenIfNull accepts at most one value; true also invokes the callback for nil values.
// Callbacks must be concurrency-safe. Tags such as omitempty still control ordinary zero values.
func WithValidation(tag string, fn gv.Func, callEvenIfNull ...bool) Option {
	return func(o *options) {
		if len(callEvenIfNull) > 1 {
			o.invalidArity = true
		}
		cv := validation{tag: tag, fn: fn}
		if len(callEvenIfNull) > 0 {
			cv.callEvenIfNull = callEvenIfNull[0]
		}
		o.validations = append(o.validations, cv)
	}
}

// WithStructValidation registers cross-field validation for the supplied struct types.
// fn must be non-nil and concurrency-safe; types must contain at least one struct value or pointer.
func WithStructValidation(fn func(gv.StructLevel), types ...any) Option {
	captured := slices.Clone(types)
	return func(o *options) {
		o.structValidations = append(o.structValidations, structValidation{fn: fn, types: captured})
	}
}

// WithValidationCtx registers a field callback receiving the Context passed to Struct or Var.
// Tag, callback, and callEvenIfNull constraints are the same as WithValidation.
// fn must be concurrency-safe and handle cancellation and timeouts itself; validation does not interrupt it.
func WithValidationCtx(tag string, fn gv.FuncCtx, callEvenIfNull ...bool) Option {
	return func(o *options) {
		if len(callEvenIfNull) > 1 {
			o.invalidArity = true
		}
		cv := validation{tag: tag, ctxFn: fn}
		if len(callEvenIfNull) == 1 {
			cv.callEvenIfNull = callEvenIfNull[0]
		}
		o.validations = append(o.validations, cv)
	}
}

// WithStructValidationCtx registers a struct callback receiving the Context passed to Struct.
// Callback and type constraints match WithStructValidation; fn handles cancellation and timeouts itself.
func WithStructValidationCtx(fn gv.StructLevelFuncCtx, types ...any) Option {
	captured := slices.Clone(types)
	return func(o *options) {
		o.structValidations = append(o.structValidations, structValidation{ctxFn: fn, types: captured})
	}
}
