package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	kiterrors "github.com/kochabx/kit/errors"
	"github.com/stretchr/testify/assert"
)

func TestOK(t *testing.T) {
	tests := []struct {
		name string
		data any
		want string
	}{
		{
			name: "string data",
			data: "test data",
			want: `{"code":200,"msg":"ok","data":"test data"}`,
		},
		{
			name: "map data",
			data: map[string]string{"key": "value"},
			want: `{"code":200,"msg":"ok","data":{"key":"value"}}`,
		},
		{
			name: "nil data",
			data: nil,
			want: `{"code":200,"msg":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			OK(w, tt.data)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, tt.want, w.Body.String())
		})
	}
}

func TestFail(t *testing.T) {
	tests := []struct {
		name string
		code int
		msg  any
		want string
	}{
		{
			name: "with kit error",
			code: 10001,
			msg:  kiterrors.New(10001, "custom error message"),
			want: `{"code":10001,"msg":"custom error message"}`,
		},
		{
			name: "with standard error",
			code: 500,
			msg:  errors.New("standard error"),
			want: `{"code":500,"msg":"standard error"}`,
		},
		{
			name: "with string message",
			code: 400,
			msg:  "bad request",
			want: `{"code":400,"msg":"bad request"}`,
		},
		{
			name: "with nil",
			code: 500,
			msg:  nil,
			want: `{"code":500,"msg":"failed"}`,
		},
		{
			name: "with data object",
			code: 201,
			msg:  map[string]any{"id": 123},
			want: `{"code":201,"data":{"id":123}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			Fail(w, tt.code, tt.msg)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, tt.want, w.Body.String())
		})
	}
}

func TestOKAndFail_NilWriter(t *testing.T) {
	// must not panic
	OK(nil, "test")
	Fail(nil, 500, "error")
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "failed",
		},
		{
			name: "kit error",
			err:  kiterrors.New(10001, "custom message"),
			want: "custom message",
		},
		{
			name: "standard error",
			err:  errors.New("standard message"),
			want: "standard message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractErrorMessage(tt.err))
		})
	}
}

func BenchmarkOK(b *testing.B) {
	w := httptest.NewRecorder()
	testData := map[string]string{"key": "value"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		OK(w, testData)
	}
}

func BenchmarkFail(b *testing.B) {
	w := httptest.NewRecorder()
	testErr := kiterrors.New(500, "benchmark error")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		Fail(w, 500, testErr)
	}
}

// ---------------------------------------------------------------------------
// Multi-framework integration tests
// ---------------------------------------------------------------------------

// TestOK_WithStdlibHandler verifies OK() works when the writer comes from a
// plain net/http handler.
func TestOK_WithStdlibHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		OK(w, map[string]int{"count": 3})
	})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/data", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"code":200,"msg":"ok","data":{"count":3}}`, w.Body.String())
}

// TestFail_WithStdlibHandler verifies Fail() works inside a stdlib handler.
func TestFail_WithStdlibHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/err", func(w http.ResponseWriter, r *http.Request) {
		Fail(w, 400, errors.New("bad input"))
	})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/err", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"code":400,"msg":"bad input"}`, w.Body.String())
}

// TestOK_WithGin verifies OK() works inside a Gin handler using its
// ResponseWriter adapter.
func TestOK_WithGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/items", func(c *gin.Context) {
		OK(c.Writer, []string{"a", "b"})
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/items", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"code":200,"msg":"ok","data":["a","b"]}`, w.Body.String())
}

// TestFail_WithGin_KitError verifies Fail() extracts the message from a kit
// error when running inside Gin.
func TestFail_WithGin_KitError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/fail", func(c *gin.Context) {
		Fail(c.Writer, 10001, kiterrors.New(10001, "resource not found"))
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"code":10001,"msg":"resource not found"}`, w.Body.String())
}

// TestFail_WithGin_StringMsg verifies Fail() with a plain string message inside Gin.
func TestFail_WithGin_StringMsg(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/submit", func(c *gin.Context) {
		Fail(c.Writer, 422, "validation error")
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/submit", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"code":422,"msg":"validation error"}`, w.Body.String())
}

// TestOK_WithGin_NilData verifies OK() omits the data field when nil is passed.
func TestOK_WithGin_NilData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.DELETE("/resource", func(c *gin.Context) {
		OK[any](c.Writer, nil)
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/resource", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"code":200,"msg":"ok"}`, w.Body.String())
}

// TestOK_ContentType_AcrossFrameworks checks that the Content-Type header is
// set consistently regardless of which framework provides the ResponseWriter.
func TestOK_ContentType_AcrossFrameworks(t *testing.T) {
	const wantCT = "application/json; charset=utf-8"

	t.Run("stdlib", func(t *testing.T) {
		w := httptest.NewRecorder()
		OK(w, "x")
		assert.Equal(t, wantCT, w.Header().Get("Content-Type"))
	})

	t.Run("gin", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.GET("/ct", func(c *gin.Context) { OK(c.Writer, "x") })
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ct", nil))
		assert.Equal(t, wantCT, w.Header().Get("Content-Type"))
	})
}
