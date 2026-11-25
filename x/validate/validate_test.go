package validate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	gv "github.com/go-playground/validator/v10"

	"github.com/kochabx/kit/fault"
)

type localeKey struct{}

type signupReq struct {
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=18"`
	Code  string `json:"code" validate:"nonempty_code"`
}

func TestLocaleENAndZHWithoutLeakingValues(t *testing.T) {
	v, err := New(
		WithLocaleExtractor(func(ctx context.Context) (Locale, bool) {
			l, ok := ctx.Value(localeKey{}).(Locale)
			return l, ok
		}),
		WithValidation("nonempty_code", func(fl gv.FieldLevel) bool {
			return strings.HasPrefix(fl.Field().String(), "KIT-")
		}),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	bad := signupReq{Email: "secret-bad-email", Age: 12, Code: "SECRET999"}

	errEN := v.Struct(t.Context(), bad)
	fErr, ok := fault.From(errEN)
	if !ok || fErr.HTTPStatus() != http.StatusBadRequest || fErr.Code() != fault.CodeInvalidArgument {
		t.Fatalf("expected fault.InvalidArgument, got %v", errEN)
	}
	if strings.Contains(fErr.Message(), "secret-bad-email") || strings.Contains(fErr.Message(), "SECRET999") {
		t.Fatalf("error message leaked raw field values: %q", fErr.Message())
	}
	viols, ok := Violations(errEN)
	if !ok || len(viols) != 3 {
		t.Fatalf("Violations = (%+v, %v), want 3", viols, ok)
	}
	if viols[0].Field != "email" {
		t.Fatalf("expected json tag field name email, got %q", viols[0].Field)
	}

	ctxZH := context.WithValue(t.Context(), localeKey{}, LocaleZH)
	errZH := v.Struct(ctxZH, bad)
	fZH, ok := fault.From(errZH)
	if !ok || !strings.Contains(fZH.Message(), "有效的邮箱") {
		t.Fatalf("expected Chinese translation, got %q", errZH)
	}
}

func TestVarRequiredEnabledAndOptionErrors(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Var(t.Context(), "not-an-email", "email"); err == nil {
		t.Fatal("expected Var validation error")
	}

	type requiredBool struct {
		Accepted bool `json:"accepted" validate:"required"`
	}
	if err := v.Struct(t.Context(), requiredBool{Accepted: false}); err == nil {
		t.Fatal("WithRequiredStructEnabled must reject zero bool when validate:required")
	}
	if err := v.Struct(t.Context(), requiredBool{Accepted: true}); err != nil {
		t.Fatalf("unexpected error for valid requiredBool: %v", err)
	}

	if _, err := New(nil); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("expected ErrInvalidOption, got %v", err)
	}
	if _, err := New(WithDefaultLocale("fr")); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("expected ErrInvalidOption for disabled default locale, got %v", err)
	}
}
