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

func TestGinJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		data any
		want string
	}{
		{
			name: "string data",
			data: "test data",
			want: `{"code":200,"msg":"success","data":"test data"}`,
		},
		{
			name: "map data",
			data: map[string]string{"key": "value"},
			want: `{"code":200,"msg":"success","data":{"key":"value"}}`,
		},
		{
			name: "nil data",
			data: nil,
			want: `{"code":200,"msg":"success"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			GinJSON(c, tt.data)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, tt.want, w.Body.String())
		})
	}
}

func TestGinJSONE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		code int
		data any
		want string
	}{
		{
			name: "with kit error",
			code: 10001,
			data: kiterrors.New(10001, "custom error message"),
			want: `{"code":10001,"msg":"custom error message"}`,
		},
		{
			name: "with standard error",
			code: 500,
			data: errors.New("standard error"),
			want: `{"code":500,"msg":"standard error"}`,
		},
		{
			name: "with string message",
			code: 400,
			data: "bad request",
			want: `{"code":400,"msg":"bad request"}`,
		},
		{
			name: "with nil",
			code: 500,
			data: nil,
			want: `{"code":500,"msg":"operation failed"}`,
		},
		{
			name: "with data object",
			code: 201,
			data: map[string]any{"id": 123},
			want: `{"code":201,"data":{"id":123}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			GinJSONE(c, tt.code, tt.data)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, tt.want, w.Body.String())
		})
	}
}

func TestGinJSONWithNilContext(t *testing.T) {
	// 应该不会 panic
	GinJSON(nil, "test")
	GinJSONE(nil, 500, "error")
}

func TestSuccess(t *testing.T) {
	resp := Success("test data")

	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, "success", resp.Msg)
	assert.Equal(t, "test data", resp.Data)
}

func TestFailure(t *testing.T) {
	resp := Failure(404, "not found")

	assert.Equal(t, 404, resp.Code)
	assert.Equal(t, "not found", resp.Msg)
	assert.Nil(t, resp.Data)
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
			want: "operation failed",
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
			got := extractErrorMessage(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Benchmark 测试
func BenchmarkGinJSON(b *testing.B) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	testData := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		GinJSON(c, testData)
	}
}

func BenchmarkGinJSONE(b *testing.B) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	testErr := kiterrors.New(500, "benchmark error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		GinJSONE(c, 500, testErr)
	}
}
