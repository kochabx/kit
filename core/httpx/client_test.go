package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type echo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query"`
	Body   string `json:"body"`
	CT     string `json:"ct"`
	Auth   string `json:"auth"`
}

func newEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := echo{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(body),
			CT:     r.Header.Get("Content-Type"),
			Auth:   r.Header.Get("Authorization"),
		}
		w.Header().Set("Content-Type", ContentTypeJSON)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}
func TestClient_GetWithInto(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	c := New()
	var got echo
	resp, err := c.Get(context.Background(), srv.URL+"/users",
		Query("page", "1"),
		Query("limit", "10"),
		Into(&got),
	)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got.Method != "GET" || got.Path != "/users" {
		t.Errorf("echo = %+v", got)
	}
	if !strings.Contains(got.Query, "page=1") || !strings.Contains(got.Query, "limit=10") {
		t.Errorf("query = %q", got.Query)
	}
}

func TestClient_PostJSON_AutoContentType(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	c := New()
	var got echo
	_, err := c.Post(context.Background(), srv.URL+"/x",
		JSON(map[string]string{"a": "b"}),
		Into(&got),
	)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	if got.CT != ContentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got.CT, ContentTypeJSON)
	}
	if !strings.Contains(got.Body, `"a":"b"`) {
		t.Errorf("body = %q", got.Body)
	}
}

func TestClient_PostForm_AutoContentType(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	c := New()
	var got echo
	_, err := c.Post(context.Background(), srv.URL+"/f",
		FormMap(map[string]string{"name": "alice"}),
		Into(&got),
	)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	if got.CT != ContentTypeForm {
		t.Errorf("Content-Type = %q, want %q", got.CT, ContentTypeForm)
	}
	if got.Body != "name=alice" {
		t.Errorf("body = %q", got.Body)
	}
}

func TestClient_GetNoContentType(t *testing.T) {
	// GET 无 body 时不应自动带 Content-Type
	srv := newEchoServer(t)
	defer srv.Close()

	c := New()
	var got echo
	_, err := c.Get(context.Background(), srv.URL+"/g", Into(&got))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.CT != "" {
		t.Errorf("expected empty Content-Type, got %q", got.CT)
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := New()
	resp, err := c.Get(context.Background(), srv.URL+"/missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if herr.StatusCode != 404 {
		t.Errorf("StatusCode = %d", herr.StatusCode)
	}
	if string(herr.Body) != "not found" {
		t.Errorf("body = %q", herr.Body)
	}
	if resp == nil || resp.StatusCode != 404 {
		t.Errorf("resp should be preserved with status 404")
	}
}

func TestClient_HTTPError_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := New(WithErrorOnStatus(nil))
	resp, err := c.Get(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestClient_BaseURL(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var got echo
	_, err := c.Get(context.Background(), "/api/v1/users", Into(&got))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Path != "/api/v1/users" {
		t.Errorf("path = %q", got.Path)
	}
}

func TestClient_DefaultHeader_Override(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-UA", r.Header.Get("User-Agent"))
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New(WithDefaultHeader("User-Agent", "default-ua"))
	resp, err := c.Get(context.Background(), srv.URL+"/", SetHeader("User-Agent", "override"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-UA"); got != "override" {
		t.Errorf("UA = %q", got)
	}
}

func TestClient_BasicAuthAndBearer(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	c := New()
	var got echo
	_, err := c.Get(context.Background(), srv.URL+"/", Bearer("tok"), Into(&got))
	if err != nil || got.Auth != "Bearer tok" {
		t.Errorf("bearer err=%v auth=%q", err, got.Auth)
	}

	got = echo{}
	_, err = c.Get(context.Background(), srv.URL+"/", BasicAuth("u", "p"), Into(&got))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	// base64("u:p") = dTpw
	if got.Auth != "Basic dTpw" {
		t.Errorf("basic auth = %q", got.Auth)
	}
}

func TestClient_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Get(ctx, srv.URL+"/")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClient_Retry_Until_OK(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", ContentTypeJSON)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(WithRetry(3, nil, nil))
	var out map[string]bool
	resp, err := c.Get(context.Background(), srv.URL+"/", Into(&out))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !out["ok"] {
		t.Errorf("out = %v", out)
	}
	if c := atomic.LoadInt32(&calls); c != 3 {
		t.Errorf("calls = %d, want 3", c)
	}
}

func TestClient_Retry_Exhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := New(WithRetry(3, nil, nil))
	_, err := c.Get(context.Background(), srv.URL+"/")
	var herr *HTTPError
	if !errors.As(err, &herr) || herr.StatusCode != 503 {
		t.Fatalf("expected 503 HTTPError, got %v", err)
	}
	if c := atomic.LoadInt32(&calls); c != 3 {
		t.Errorf("calls = %d, want 3", c)
	}
}

func TestClient_Middleware_Order(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	var order []string
	mw := func(name string) Middleware {
		return func(next RoundTripFunc) RoundTripFunc {
			return func(req *http.Request) (*http.Response, error) {
				order = append(order, "->"+name)
				resp, err := next(req)
				order = append(order, name+"->")
				return resp, err
			}
		}
	}

	c := New(WithMiddleware(mw("A"), mw("B")))
	_, err := c.Get(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	want := []string{"->A", "->B", "B->", "A->"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestClient_IntoBytesAndString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	c := New()
	var b []byte
	if _, err := c.Get(context.Background(), srv.URL+"/", IntoBytes(&b)); err != nil {
		t.Fatalf("IntoBytes: %v", err)
	}
	if string(b) != "hello" {
		t.Errorf("bytes = %q", b)
	}

	var s string
	if _, err := c.Get(context.Background(), srv.URL+"/", IntoString(&s)); err != nil {
		t.Fatalf("IntoString: %v", err)
	}
	if s != "hello" {
		t.Errorf("string = %q", s)
	}
}

func TestClient_RawBody(t *testing.T) {
	srv := newEchoServer(t)
	defer srv.Close()

	c := New()
	var got echo
	_, err := c.Post(context.Background(), srv.URL+"/r",
		Raw("application/octet-stream", []byte{1, 2, 3}),
		Into(&got),
	)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}
	if got.CT != "application/octet-stream" {
		t.Errorf("CT = %q", got.CT)
	}
	if got.Body != string([]byte{1, 2, 3}) {
		t.Errorf("body = %v", []byte(got.Body))
	}
}

func TestClient_NilContext(t *testing.T) {
	c := New()
	var ctx context.Context // intentionally nil
	_, err := c.Do(ctx, "GET", "http://example.com", nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}
