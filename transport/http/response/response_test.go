package response

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/errors"
	"github.com/stretchr/testify/assert"
)

func TestGinJson(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	GinJSON(c, "test data")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"code":200`)
	assert.Contains(t, w.Body.String(), `"message":"success"`)
	assert.Contains(t, w.Body.String(), `"data":"test data"`)
}

func TestGinJsonWithError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	GinJSONE(c, errors.New(10000, "test reason"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"code":10000`)
	assert.Contains(t, w.Body.String(), `"message":"test reason"`)
}

func TestGinJsonWithValidHTTPCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// 创建一个有效HTTP状态码的错误
	GinJSONE(c, errors.New(400, "bad request"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"code":400`)
	assert.Contains(t, w.Body.String(), `"message":"bad request"`)
}

// TestBuilder tests the fluent builder pattern
func TestBuilder(t *testing.T) {
	// Test with pool
	builder := NewBuilder(true)
	resp := builder.
		WithCode(201).
		WithData("created").
		WithMessage("resource created").
		Build()

	assert.Equal(t, 201, resp.Code)
	assert.Equal(t, "created", resp.Data)
	assert.Equal(t, "resource created", resp.Message)

	// Test without pool
	builder2 := NewBuilder(false)
	resp2 := builder2.
		WithCode(404).
		WithMessage("not found").
		Build()

	assert.Equal(t, 404, resp2.Code)
	assert.Nil(t, resp2.Data)
	assert.Equal(t, "not found", resp2.Message)
}

// TestGinJSONWithBuilder tests the builder pattern with Gin
func TestGinJSONWithBuilder(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	GinJSONWithBuilder(c, func(b *Builder) {
		b.WithCode(201).
			WithData("test").
			WithMessage("created")
	})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"code":201`)
	assert.Contains(t, w.Body.String(), `"data":"test"`)
	assert.Contains(t, w.Body.String(), `"message":"created"`)
}

// TestResponsePool tests object pool functionality
func TestResponsePool(t *testing.T) {
	// Acquire and release multiple responses to test pool reuse
	responses := make([]*Response, 10)

	for i := 0; i < 10; i++ {
		responses[i] = acquireResponse()
		responses[i].Code = i
		responses[i].Data = fmt.Sprintf("data%d", i)
		responses[i].Message = fmt.Sprintf("message%d", i)
	}

	// Release all responses
	for _, resp := range responses {
		releaseResponse(resp)
	}

	// Acquire again and verify they're reset
	newResp := acquireResponse()
	assert.Equal(t, 0, newResp.Code)
	assert.Nil(t, newResp.Data)
	assert.Equal(t, "", newResp.Message)

	releaseResponse(newResp)
}

// Benchmark tests for performance optimization validation
func BenchmarkGinJSON(b *testing.B) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	testData := "benchmark test data"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		GinJSON(c, testData)
	}
}

func BenchmarkGinJSONError(b *testing.B) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	testErr := errors.New(500, "benchmark error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Body.Reset()
		GinJSONE(c, testErr)
	}
}

func BenchmarkResponseBuilder(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder := NewBuilder(true)
		_ = builder.
			WithCode(200).
			WithData("test").
			WithMessage("success").
			Build()
	}
}

func BenchmarkResponsePool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := acquireResponse()
		resp.setSuccess("test data")
		releaseResponse(resp)
	}
}

func BenchmarkResponseDirect(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := &Response{
			Code:    200,
			Data:    "test data",
			Message: "success",
		}
		_ = resp
	}
}
