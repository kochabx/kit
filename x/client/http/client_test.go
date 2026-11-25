package http

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/fault"
)

func TestGenericMethodsRetryAndFaultMapping(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/items":
			if attempts.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_ = json.MarshalWrite(w, map[string]string{
				"q":    r.URL.Query().Get("q"),
				"auth": r.Header.Get("Authorization"),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/items":
			var in map[string]string
			_ = json.UnmarshalRead(r.Body, &in)
			_ = json.MarshalWrite(w, map[string]string{"created": in["name"]})
		case r.URL.Path == "/api/missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	c, err := New(
		WithBaseURL(srv.URL+"/api/"),
		WithBearerToken("tok-1"),
		WithRetry(2, 5*time.Millisecond, 20*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Get with retry recovery + url.JoinPath normalization + query param.
	got, err := c.Get[map[string]string](t.Context(), "/items", WithQuery("q", "go127"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if attempts.Load() != 2 || got["q"] != "go127" || got["auth"] != "Bearer tok-1" {
		t.Fatalf("unexpected Get result: attempts=%d got=%v", attempts.Load(), got)
	}

	// Post typed JSON round-trip.
	created, err := c.Post[map[string]string, map[string]string](t.Context(), "items", map[string]string{"name": "kit"})
	if err != nil || created["created"] != "kit" {
		t.Fatalf("Post = (%v, %v)", created, err)
	}

	// 404 maps to *fault.Error NotFound.
	_, err = c.Get[map[string]string](t.Context(), "/missing")
	f, ok := fault.From(err)
	if !ok || f.Code() != fault.CodeNotFound || f.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("expected fault.NotFound, got %v", err)
	}

	// Inline query strings in path must not be percent-encoded to %3F.
	gotQ, err := c.Get[map[string]string](t.Context(), "/items?q=inline-query")
	if err != nil || gotQ["q"] != "inline-query" {
		t.Fatalf("inline query string failed: got=%v err=%v", gotQ, err)
	}
}

type wrappedRoundTripper struct{ next http.RoundTripper }

func (w wrappedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return w.next.RoundTrip(r)
}

func TestNewWithWrappedDefaultTransport(t *testing.T) {
	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })

	http.DefaultTransport = wrappedRoundTripper{next: orig}

	c, err := New()
	if err != nil {
		t.Fatalf("New() with wrapped DefaultTransport failed: %v", err)
	}
	defer c.Close()

	if _, err := New(WithTLSConfig(&tls.Config{})); err == nil {
		t.Fatal("expected error when applying WithTLSConfig to non-*http.Transport DefaultTransport")
	}
}

func TestContextCancellationMappedToFaultCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c, err := New(WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	cancelCtx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = c.Get[map[string]string](cancelCtx, "/slow")
	if f, ok := fault.From(err); !ok || f.Code() != fault.CodeCanceled {
		t.Fatalf("canceled request fault = %v, want %s", err, fault.CodeCanceled)
	}

	deadlineCtx, deadlineCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer deadlineCancel()
	_, err = c.Get[map[string]string](deadlineCtx, "/slow")
	if f, ok := fault.From(err); !ok || f.Code() != fault.CodeDeadlineExceeded {
		t.Fatalf("timed out request fault = %v, want %s", err, fault.CodeDeadlineExceeded)
	}
}
