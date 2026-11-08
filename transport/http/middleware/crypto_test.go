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
// Crypto middleware tests.
// Verify consistent behavior with net/http and Gin.
// ============================================================================

// identityDecryptor returns its input unchanged for testing.
var identityDecryptor = DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
})

// failDecryptor always returns an error.
var failDecryptor = DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
	return nil, errors.New("decrypt failed")
})

func TestCrypto_Success(t *testing.T) {
	plaintext := `{"user":"alice"}`
	// This test sends Base64-encoded ciphertext.
	// The identity decryptor returns its input unchanged; the request contains Base64 plaintext.
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

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), `"code"`) {
		t.Errorf("response should contain error code, got: %s", w.Body.String())
	}
}

func TestCrypto_EmptyBody(t *testing.T) {
	// An empty body should pass through without invoking the decryptor.
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

	// Expect malformed Base64 to invoke ErrorHandler.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (Fail uses business code)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"code"`) {
		t.Errorf("response should contain error code, got: %s", w.Body.String())
	}
}

func TestCrypto_SkipPaths(t *testing.T) {
	// Skipped requests retain their original body.
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
// Gin integration test.
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
