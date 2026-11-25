package fault

import (
	"encoding/json/v2"
	stderrors "errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"
)

func TestConstructorsAndMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        *Error
		wantCode   Code
		wantHTTP   int
		wantGRPC   uint32
		wantSubstr string
	}{
		{"invalid_arg", InvalidArgument("bad param"), CodeInvalidArgument, http.StatusBadRequest, grpcInvalidArgument, "code=INVALID_ARGUMENT"},
		{"bad_request", BadRequest("bad req"), CodeInvalidArgument, http.StatusBadRequest, grpcInvalidArgument, "status=400"},
		{"unauthorized", Unauthorized("no token"), CodeUnauthenticated, http.StatusUnauthorized, grpcUnauthenticated, "status=401"},
		{"forbidden", Forbidden("denied"), CodePermissionDenied, http.StatusForbidden, grpcPermissionDenied, "status=403"},
		{"not_found", NotFound("missing"), CodeNotFound, http.StatusNotFound, grpcNotFound, "status=404"},
		{"conflict", Conflict("dup"), CodeConflict, http.StatusConflict, grpcAborted, "status=409"},
		{"unprocessable", UnprocessableEntity("invalid"), CodeInvalidArgument, http.StatusUnprocessableEntity, grpcInvalidArgument, "status=422"},
		{"too_many", TooManyRequests("slow down"), CodeTooManyRequests, http.StatusTooManyRequests, grpcResourceExhausted, "status=429"},
		{"internal", Internal("boom"), CodeInternal, http.StatusInternalServerError, grpcInternal, "status=500"},
		{"not_impl", NotImplemented("todo"), CodeNotImplemented, http.StatusNotImplemented, grpcUnimplemented, "status=501"},
		{"bad_gw", BadGateway("upstream"), CodeBadGateway, http.StatusBadGateway, grpcUnavailable, "status=502"},
		{"unavail", ServiceUnavailable("busy"), CodeUnavailable, http.StatusServiceUnavailable, grpcUnavailable, "status=503"},
		{"gw_timeout", GatewayTimeout("slow"), CodeGatewayTimeout, http.StatusGatewayTimeout, grpcDeadlineExceeded, "status=504"},
		{"canceled", Canceled("aborted"), CodeCanceled, 499, grpcCanceled, "status=499"},
		{"deadline", DeadlineExceeded("timeout"), CodeDeadlineExceeded, http.StatusGatewayTimeout, grpcDeadlineExceeded, "code=DEADLINE_EXCEEDED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code() != tt.wantCode {
				t.Fatalf("Code() = %q, want %q", tt.err.Code(), tt.wantCode)
			}
			if tt.err.HTTPStatus() != tt.wantHTTP {
				t.Fatalf("HTTPStatus() = %d, want %d", tt.err.HTTPStatus(), tt.wantHTTP)
			}
			if tt.err.GRPCCode() != tt.wantGRPC {
				t.Fatalf("GRPCCode() = %d, want %d", tt.err.GRPCCode(), tt.wantGRPC)
			}
			if !strings.Contains(tt.err.Error(), tt.wantSubstr) {
				t.Fatalf("Error() = %q missing %q", tt.err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestImmutabilityAndCauseChaining(t *testing.T) {
	rootCause := stderrors.New("dial tcp 10.0.0.1:5432: connection refused")
	base := NotFound("order missing").With("field", "id")
	meta := map[string]string{"tenant": "acme", "region": "ap"}
	derived := base.WithMetadata(meta).WithCause(rootCause).WithStatus(http.StatusGone)
	meta["tenant"] = "mutated"

	if base.Unwrap() != nil || base.HTTPStatus() != http.StatusNotFound || len(base.Metadata()) != 1 {
		t.Fatalf("base error was mutated: %+v", base)
	}
	if !stderrors.Is(derived, rootCause) {
		t.Fatal("derived error does not unwrap to rootCause")
	}
	wrapped := fmt.Errorf("handler failed: %w", derived)
	got, ok := From(wrapped)
	if !ok || got != derived {
		t.Fatalf("From(wrapped) = (%v, %v), want %v", got, ok, derived)
	}
	if CodeOf(wrapped) != CodeNotFound || HTTPStatusOf(wrapped) != http.StatusGone {
		t.Fatalf("CodeOf=%q HTTPStatusOf=%d", CodeOf(wrapped), HTTPStatusOf(wrapped))
	}

	gotMeta := derived.Metadata()
	wantMeta := map[string]string{"field": "id", "tenant": "acme", "region": "ap"}
	if !maps.Equal(gotMeta, wantMeta) {
		t.Fatalf("Metadata() = %v, want %v", gotMeta, wantMeta)
	}
	gotMeta["field"] = "tampered"
	if derived.Metadata()["field"] != "id" {
		t.Fatal("Metadata() did not return a defensive copy")
	}
}

func TestProblemJSONv2AndRedaction(t *testing.T) {
	secretErr := stderrors.New("sql: pq password leak")
	err := Newf(CodeAlreadyExists, "user %s exists", "alice").
		With("field", "email").
		WithCause(secretErr)

	prob := err.Problem("/v1/users")
	raw, marshalErr := json.Marshal(prob)
	if marshalErr != nil {
		t.Fatalf("json.Marshal(Problem) failed: %v", marshalErr)
	}
	text := string(raw)
	if strings.Contains(text, "sql:") || strings.Contains(text, "password leak") {
		t.Fatalf("Problem leaked internal cause: %s", text)
	}

	var decoded Problem
	if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(Problem) failed: %v", unmarshalErr)
	}
	if decoded.Status != http.StatusConflict || decoded.Code != CodeAlreadyExists || decoded.Instance != "/v1/users" {
		t.Fatalf("unexpected decoded Problem: %+v", decoded)
	}
}

func TestNilErrorSafety(t *testing.T) {
	var e *Error
	if e.Error() != "" || e.Code() != "" || e.Message() != "" || e.Unwrap() != nil || e.Metadata() != nil {
		t.Fatal("nil *Error methods did not return zero values")
	}
	if e.HTTPStatus() != http.StatusOK || e.GRPCCode() != grpcOK {
		t.Fatal("nil *Error status mapping unexpected")
	}
	if e.With("a", "b") != nil || e.WithCause(stderrors.New("x")) != nil || e.WithStatus(400) != nil {
		t.Fatal("nil *Error mutators should return nil")
	}
	if CodeOf(nil) != "" || HTTPStatusOf(nil) != http.StatusOK {
		t.Fatal("nil error helpers unexpected")
	}
	if plain := stderrors.New("plain"); CodeOf(plain) != CodeInternal || HTTPStatusOf(plain) != http.StatusInternalServerError {
		t.Fatal("plain error helpers unexpected")
	}
}

func BenchmarkFaultProblemMarshal(b *testing.B) {
	err := InvalidArgument("bad input").With("field", "name").WithCause(stderrors.New("parse error"))
	for b.Loop() {
		_, _ = json.Marshal(err.Problem("/items"))
	}
}
