package middleware

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// Crypto 中间件测试
// 展示：stdlib net/http 与 Gin 两种框架下的一致行为
// ============================================================================

// identityDecryptor 身份解密器：原样返回，用于测试
var identityDecryptor = DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
})

// failDecryptor 失败解密器：始终返回错误
var failDecryptor = DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
	return nil, errors.New("decrypt failed")
})

func TestCrypto_Success(t *testing.T) {
	plaintext := `{"user":"alice"}`
	// 因为 Base64Decode 默认为 true，body 需要是 base64 编码的"密文"
	// 使用身份解密器时：密文 = 明文，body = base64(明文)
	encoded := base64.StdEncoding.EncodeToString([]byte(plaintext))

	mw := Crypto(CryptoConfig{Decryptor: identityDecryptor})

	var gotBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(encoded))
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotBody != plaintext {
		t.Errorf("body = %q, want %q", gotBody, plaintext)
	}
}

func TestCrypto_DecryptFail(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("some data"))
	mw := Crypto(CryptoConfig{Decryptor: failDecryptor})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(encoded))
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (Fail uses business code)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"code"`) {
		t.Errorf("response should contain error code, got: %s", w.Body.String())
	}
}

func TestCrypto_EmptyBody(t *testing.T) {
	// 空 body 应直接透传，不调用解密器
	called := false
	d := DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
		called = true
		return ciphertext, nil
	})
	mw := Crypto(CryptoConfig{Decryptor: d})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if called {
		t.Error("decryptor should not be called for empty body")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCrypto_InvalidBase64(t *testing.T) {
	mw := Crypto(CryptoConfig{Decryptor: identityDecryptor})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-valid-base64!!!"))
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	// base64 解码失败，触发 ErrorHandler
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (Fail uses business code)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"code"`) {
		t.Errorf("response should contain error code, got: %s", w.Body.String())
	}
}

func TestCrypto_SkipPaths(t *testing.T) {
	// 在跳过的路径上，body 不经过解密
	var gotBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	mw := Crypto(CryptoConfig{
		Decryptor: failDecryptor,
		Skip:      SkipConfig{Paths: []string{"/public"}},
	})

	rawBody := "raw-unencrypted-body"
	req := httptest.NewRequest(http.MethodPost, "/public", strings.NewReader(rawBody))
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotBody != rawBody {
		t.Errorf("skip path should not decrypt body, got %q", gotBody)
	}
}

func TestCrypto_CustomErrorHandler(t *testing.T) {
	customCalled := false
	encoded := base64.StdEncoding.EncodeToString([]byte("data"))
	mw := Crypto(CryptoConfig{
		Decryptor: failDecryptor,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			customCalled = true
			w.WriteHeader(http.StatusUnprocessableEntity)
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(encoded))
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if !customCalled {
		t.Error("custom error handler should be called")
	}
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

// ============================================================================
// Gin 集成测试
// ============================================================================

func TestCrypto_Gin(t *testing.T) {
	plaintext := "hello gin"
	encoded := base64.StdEncoding.EncodeToString([]byte(plaintext))

	var gotBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	r := ginEngine(http.MethodPost, "/decrypt",
		Crypto(CryptoConfig{Decryptor: identityDecryptor}),
		inner,
	)

	req := httptest.NewRequest(http.MethodPost, "/decrypt", strings.NewReader(encoded))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[Gin] status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotBody != plaintext {
		t.Errorf("[Gin] body = %q, want %q", gotBody, plaintext)
	}
}
