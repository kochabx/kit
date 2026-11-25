// Package validate provides an instance-scoped, i18n-capable struct/field validator
// backed by validator/v10 and mapped to *fault.Error without exposing raw values.
package validate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-playground/locales"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	gv "github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"

	"github.com/kochabx/kit/fault"
)

// Locale identifies a translation language supported by Validator.
type Locale string

const (
	LocaleEN Locale = "en"
	LocaleZH Locale = "zh"
)

var ErrInvalidOption = errors.New("invalid validator option")

// Violation describes one translated field constraint failure without its raw value.
type Violation struct {
	Field   string `json:"field,omitzero"`
	Tag     string `json:"tag"`
	Param   string `json:"param,omitzero"`
	Path    string `json:"path,omitzero"`
	Message string `json:"message"`
}

// ValidationError carries structured violations as the internal cause of *fault.Error.
type ValidationError struct {
	violations []Violation
	message    string
	cause      error
}

func (e *ValidationError) Error() string           { return e.message }
func (e *ValidationError) Unwrap() error           { return e.cause }
func (e *ValidationError) Violations() []Violation { return slices.Clone(e.violations) }

// Violations extracts structured field violations from err when present.
func Violations(err error) ([]Violation, bool) {
	ve, ok := errors.AsType[*ValidationError](err)
	if !ok {
		return nil, false
	}
	return ve.Violations(), true
}

// LocaleEntry pairs a CLDR locale translator with its validator translation registrar.
type LocaleEntry struct {
	Loc      locales.Translator
	Register func(*gv.Validate, ut.Translator) error
}

type validationRule struct {
	tag            string
	fn             gv.Func
	ctxFn          gv.FuncCtx
	callEvenIfNull bool
}

type structRule struct {
	fn    func(gv.StructLevel)
	ctxFn gv.StructLevelFuncCtx
	types []any
}

type options struct {
	fieldNameTag      string
	defaultLocale     Locale
	locales           map[Locale]LocaleEntry
	localeExtractor   func(context.Context) (Locale, bool)
	validations       []validationRule
	structValidations []structRule
	invalidOptErr     error
}

// Option customizes Validator construction.
type Option func(*options)

func WithDefaultLocale(locale Locale) Option {
	return func(o *options) { o.defaultLocale = locale }
}

func WithLocale(locale Locale, entry LocaleEntry) Option {
	return func(o *options) { o.locales[locale] = entry }
}

func WithFieldNameTag(tag string) Option {
	return func(o *options) { o.fieldNameTag = tag }
}

func WithLocaleExtractor(fn func(context.Context) (Locale, bool)) Option {
	return func(o *options) { o.localeExtractor = fn }
}

func WithValidation(tag string, fn gv.Func, callEvenIfNull ...bool) Option {
	return func(o *options) {
		if len(callEvenIfNull) > 1 {
			o.invalidOptErr = fmt.Errorf("%w: callEvenIfNull accepts at most 1 value", ErrInvalidOption)
			return
		}
		r := validationRule{tag: tag, fn: fn}
		if len(callEvenIfNull) == 1 {
			r.callEvenIfNull = callEvenIfNull[0]
		}
		o.validations = append(o.validations, r)
	}
}

func WithValidationCtx(tag string, fn gv.FuncCtx, callEvenIfNull ...bool) Option {
	return func(o *options) {
		if len(callEvenIfNull) > 1 {
			o.invalidOptErr = fmt.Errorf("%w: callEvenIfNull accepts at most 1 value", ErrInvalidOption)
			return
		}
		r := validationRule{tag: tag, ctxFn: fn}
		if len(callEvenIfNull) == 1 {
			r.callEvenIfNull = callEvenIfNull[0]
		}
		o.validations = append(o.validations, r)
	}
}

func WithStructValidation(fn func(gv.StructLevel), types ...any) Option {
	captured := slices.Clone(types)
	return func(o *options) {
		o.structValidations = append(o.structValidations, structRule{fn: fn, types: captured})
	}
}

func WithStructValidationCtx(fn gv.StructLevelFuncCtx, types ...any) Option {
	captured := slices.Clone(types)
	return func(o *options) {
		o.structValidations = append(o.structValidations, structRule{ctxFn: fn, types: captured})
	}
}

// Validator validates structs and single values with i18n message translation.
type Validator struct {
	v               *gv.Validate
	translators     map[Locale]ut.Translator
	defaultLocale   Locale
	localeExtractor func(context.Context) (Locale, bool)
}

// New constructs an isolated Validator instance with English and Simplified Chinese
// translations registered by default.
func New(opts ...Option) (*Validator, error) {
	o := &options{
		fieldNameTag:  "json",
		defaultLocale: LocaleEN,
		locales: map[Locale]LocaleEntry{
			LocaleEN: {Loc: en.New(), Register: en_translations.RegisterDefaultTranslations},
			LocaleZH: {Loc: zh.New(), Register: zh_translations.RegisterDefaultTranslations},
		},
	}
	for i, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, i)
		}
		opt(o)
		if o.invalidOptErr != nil {
			return nil, o.invalidOptErr
		}
	}

	v := gv.New(gv.WithRequiredStructEnabled())
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

	defEntry, ok := o.locales[o.defaultLocale]
	if !ok || defEntry.Loc == nil || defEntry.Register == nil {
		return nil, fmt.Errorf("%w: invalid default locale %q", ErrInvalidOption, o.defaultLocale)
	}
	locs := make([]locales.Translator, 0, len(o.locales))
	for loc, entry := range o.locales {
		if entry.Loc == nil || entry.Register == nil {
			return nil, fmt.Errorf("%w: incomplete locale %q", ErrInvalidOption, loc)
		}
		locs = append(locs, entry.Loc)
	}
	uni := ut.New(defEntry.Loc, locs...)
	translators := make(map[Locale]ut.Translator, len(o.locales))
	for loc, entry := range o.locales {
		trans, found := uni.GetTranslator(entry.Loc.Locale())
		if !found {
			return nil, fmt.Errorf("%w: translator not found for %q", ErrInvalidOption, loc)
		}
		if err := entry.Register(v, trans); err != nil {
			return nil, fmt.Errorf("%w: register %s translations: %w", ErrInvalidOption, loc, err)
		}
		translators[loc] = trans
	}

	for _, r := range o.validations {
		if r.tag == "" || (r.fn == nil && r.ctxFn == nil) {
			return nil, fmt.Errorf("%w: invalid validation rule %q", ErrInvalidOption, r.tag)
		}
		var err error
		if r.ctxFn != nil {
			err = v.RegisterValidationCtx(r.tag, r.ctxFn, r.callEvenIfNull)
		} else {
			err = v.RegisterValidation(r.tag, r.fn, r.callEvenIfNull)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: register %q: %w", ErrInvalidOption, r.tag, err)
		}
	}
	for _, sr := range o.structValidations {
		if (sr.fn == nil && sr.ctxFn == nil) || len(sr.types) == 0 {
			return nil, fmt.Errorf("%w: invalid struct validation", ErrInvalidOption)
		}
		if sr.ctxFn != nil {
			v.RegisterStructValidationCtx(sr.ctxFn, sr.types...)
		} else {
			v.RegisterStructValidation(sr.fn, sr.types...)
		}
	}

	return &Validator{
		v:               v,
		translators:     translators,
		defaultLocale:   o.defaultLocale,
		localeExtractor: o.localeExtractor,
	}, nil
}

// Translator exposes the translator for locale so custom error translation helpers can register rules.
func (val *Validator) Translator(locale Locale) (ut.Translator, bool) {
	t, ok := val.translators[locale]
	return t, ok
}

// Struct validates struct s and returns a *fault.Error on constraint failures.
func (val *Validator) Struct(ctx context.Context, s any) error {
	return val.wrap(ctx, val.v.StructCtx(ctx, s))
}

// Var validates a single variable value against tag and returns a *fault.Error on failure.
func (val *Validator) Var(ctx context.Context, field any, tag string) error {
	return val.wrap(ctx, val.v.VarCtx(ctx, field, tag))
}

func (val *Validator) wrap(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	verrs, ok := errors.AsType[gv.ValidationErrors](err)
	if !ok {
		return fault.InvalidArgument("validation failed").WithCause(err)
	}

	loc := val.defaultLocale
	if val.localeExtractor != nil && ctx != nil {
		if extracted, found := val.localeExtractor(ctx); found {
			loc = extracted
		}
	}
	trans := val.translators[loc]
	if trans == nil {
		trans = val.translators[val.defaultLocale]
	}

	violations := make([]Violation, len(verrs))
	msgs := make([]string, len(verrs))
	for i, fe := range verrs {
		msg := fe.Translate(trans)
		violations[i] = Violation{
			Field:   fe.Field(),
			Tag:     fe.Tag(),
			Param:   fe.Param(),
			Path:    fe.Namespace(),
			Message: msg,
		}
		msgs[i] = msg
	}

	ve := &ValidationError{
		violations: violations,
		message:    strings.Join(msgs, "; "),
		cause:      err,
	}
	return fault.InvalidArgument(ve.message).WithCause(ve)
}
