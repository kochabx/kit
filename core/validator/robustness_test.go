package validator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	gv "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
)

func TestInvalidConstructionReturnsError(t *testing.T) {
	valid := func(gv.FieldLevel) bool { return true }
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"nil option", []Option{nil}},
		{"empty locale", []Option{WithLocale("", LocaleEntry{})}},
		{
			"nil translator",
			[]Option{WithLocale("custom", LocaleEntry{Register: func(*gv.Validate, ut.Translator) error {
				return nil
			}})},
		},
		{"nil register", []Option{WithLocale("custom", LocaleEntry{Loc: en.New()})}},
		{
			"duplicate translator",
			[]Option{WithLocale("custom", LocaleEntry{
				Loc: en.New(),
				Register: func(*gv.Validate, ut.Translator) error {
					return nil
				},
			})},
		},
		{"reserved tag", []Option{WithValidation("required", valid)}},
		{"invalid tag", []Option{WithValidation("bad,tag", valid)}},
		{"duplicate tag", []Option{WithValidation("custom", valid), WithValidation("custom", valid)}},
		{"excess bool", []Option{WithValidation("custom", valid, true, false)}},
		{"nil context callback", []Option{WithValidationCtx("custom", nil)}},
		{"nil struct callback", []Option{WithStructValidation(nil, testUser{})}},
		{"missing struct types", []Option{WithStructValidation(func(gv.StructLevel) {})}},
		{"invalid struct type", []Option{WithStructValidation(func(gv.StructLevel) {}, 1)}},
		{"nil struct context callback", []Option{WithStructValidationCtx(nil, testUser{})}},
		{"panic in option", []Option{func(*options) { panic("secret") }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value Validator
			var err error
			require.NotPanics(t, func() { value, err = New(tc.opts...) })
			require.Nil(t, value)
			require.ErrorIs(t, err, ErrInvalidOption)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestErrorsPreserveCausePathAndIsolation(t *testing.T) {
	type credential struct {
		Password string `json:"password" validate:"min=20"`
	}
	type request struct {
		Users []credential `json:"users" validate:"dive"`
	}
	err := Validate.Struct(context.Background(), request{Users: []credential{{Password: "private"}}})
	ve, ok := errors.AsType[*Error](err)
	require.True(t, ok)
	raw, ok := errors.AsType[gv.ValidationErrors](err)
	require.True(t, ok)
	require.Equal(t, "private", raw[0].Value())
	require.NotContains(t, err.Error(), "private")
	violations := ve.Violations()
	require.Equal(t, "request.users[0].password", violations[0].Path)
	require.Equal(t, "password", violations[0].Field)
	violations[0].Message = "changed"
	require.NotEqual(t, "changed", ve.Violations()[0].Message)
	require.NotContains(t, ve.Error(), "changed")
}

func TestContextAwareValidations(t *testing.T) {
	type key struct{}
	type input struct {
		Name string `validate:"matchesctx"`
	}
	v := mustNew(t,
		WithValidationCtx("matchesctx", func(ctx context.Context, field gv.FieldLevel) bool {
			return field.Field().String() == ctx.Value(key{})
		}),
		WithStructValidationCtx(func(ctx context.Context, sl gv.StructLevel) {
			if ctx.Value(key{}) == "blocked" {
				sl.ReportError(sl.Current().Interface().(input).Name, "Name", "Name", "allowed", "")
			}
		}, input{}),
	)
	errs := make(chan error, 16)
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			ctx := context.WithValue(context.Background(), key{}, "allowed")
			errs <- v.Struct(ctx, input{Name: "allowed"})
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Error(t, v.Struct(context.WithValue(context.Background(), key{}, "blocked"), input{Name: "blocked"}))
	require.Error(t, v.Var(context.WithValue(context.Background(), key{}, "allowed"), "other", "matchesctx"))
}

func TestInvalidRuntimeTagRemainsProgrammingError(t *testing.T) {
	require.Panics(t, func() { _ = Validate.Var(context.Background(), "value", "unknown_validation_tag") })
	require.True(t, strings.Contains(Validate.Var(context.Background(), "bad", "email").Error(), "email"))
}

func TestLocaleRegistrationErrors(t *testing.T) {
	nilTranslator := en.New()
	nilTranslator = nil
	value, err := New(WithLocale(LocaleEN, LocaleEntry{
		Loc:      nilTranslator,
		Register: func(*gv.Validate, ut.Translator) error { return nil },
	}))
	require.Nil(t, value)
	require.ErrorIs(t, err, ErrInvalidOption)
	cause := errors.New("translation registration failed")
	value, err = New(WithLocale(LocaleEN, LocaleEntry{
		Loc:      en.New(),
		Register: func(*gv.Validate, ut.Translator) error { return cause },
	}))
	require.Nil(t, value)
	require.ErrorIs(t, err, ErrInvalidOption)
	require.ErrorIs(t, err, cause)
}
