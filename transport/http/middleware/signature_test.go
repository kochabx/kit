package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// Signature 中间件测试
// 展示：stdlib net/http 与 Gin 两种框架下的一致行为
// ============================================================================

const testSecret = "test-secret-key"

// computeHMAC 计算 HMAC-SHA256 签名（与 HMACSHA256Signer 逻辑一致）
func computeHMAC(data []byte) string {
	h := hmac.New(sha256.New, []byte(testSecret))
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestSignature_Valid_BodyOnly(t *testing.T) {
	body := `{"action":"pay","amount":100}`
	sig := computeHMAC([]byte(body))

	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Body: true},
	})

	req := httptest.NewRequest(http.MethodPost, "/pay", strings.NewReader(body))
	req.Header.Set("X-Signature", sig)
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSignature_Valid_QueryOnly(t *testing.T) {
	params := "a=1&b=2" // 已按字母序
	sig := computeHMAC([]byte(params))

	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Params: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/?a=1&b=2", nil)
	req.Header.Set("X-Signature", sig)
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSignature_MissingHeader(t *testing.T) {
	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Body: true},
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body"))
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	// Fail → HTTP 200 + 业务错误码
	if !containsString(w.Body.String(), `"code"`) {
		t.Errorf("missing signature header should return error code, got: %s", w.Body.String())
	}
}

func TestSignature_Invalid(t *testing.T) {
	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Body: true},
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body"))
	req.Header.Set("X-Signature", "wrong-signature")
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if !containsString(w.Body.String(), `"code"`) {
		t.Errorf("invalid signature should return error code, got: %s", w.Body.String())
	}
}

func TestSignature_SkipPaths(t *testing.T) {
	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Body: true},
		Skip:   SkipConfig{Paths: []string{"/health"}},
	})

	// 无签名头，但路径被跳过，应直接通过
	w := do(mw(okHandler), http.MethodGet, "/health", nil)
	if w.Code != http.StatusOK {
		t.Errorf("skipped path should pass without signature, status = %d", w.Code)
	}
}

func TestSignature_SkipFunc(t *testing.T) {
	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Body: true},
		Skip: SkipConfig{Func: func(r *http.Request) bool {
			return r.Header.Get("X-Internal-Call") == "1"
		}},
	})

	w := do(mw(okHandler), http.MethodPost, "/api", func(r *http.Request) {
		r.Header.Set("X-Internal-Call", "1")
	})
	if w.Code != http.StatusOK {
		t.Errorf("SkipFunc should bypass signature check, status = %d", w.Code)
	}
}

func TestSignature_CustomHeaderName(t *testing.T) {
	body := "payload"
	sig := computeHMAC([]byte(body))

	mw := Signature(SignatureConfig{
		Signer:     HMACSHA256Signer(testSecret),
		HeaderName: "X-My-Sig",
		Parts:      SignatureParts{Body: true},
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-My-Sig", sig)
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("custom header name should work, status = %d", w.Code)
	}
}

func TestSignature_CustomErrorHandler(t *testing.T) {
	customCalled := false
	mw := Signature(SignatureConfig{
		Signer: HMACSHA256Signer(testSecret),
		Parts:  SignatureParts{Body: true},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			customCalled = true
			w.WriteHeader(http.StatusUnauthorized)
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("data"))
	req.Header.Set("X-Signature", "bad")
	w := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(w, req)

	if !customCalled {
		t.Error("custom error handler should be called")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ============================================================================
// HMACSHA256Signer 单元测试
// ============================================================================

func TestHMACSHA256Signer_Valid(t *testing.T) {
	signer := HMACSHA256Signer(testSecret)
	data := []byte("test data")
	sig := computeHMAC(data)
	if err := signer.Verify(data, sig); err != nil {
		t.Errorf("valid signature should pass: %v", err)
	}
}

func TestHMACSHA256Signer_Invalid(t *testing.T) {
	signer := HMACSHA256Signer(testSecret)
	if err := signer.Verify([]byte("data"), "bad-sig"); err == nil {
		t.Error("invalid signature should fail")
	}
}

func TestSignerFunc_Adapter(t *testing.T) {
	fn := SignerFunc(func(data []byte, sig string) error {
		return nil
	})
	if err := fn.Verify([]byte("any"), "sig"); err != nil {
		t.Errorf("SignerFunc adapter: %v", err)
	}
}

// ============================================================================
// Gin 集成测试
// ============================================================================

func TestSignature_Gin(t *testing.T) {
	body := `{"gin":"test"}`
	sig := computeHMAC([]byte(body))

	r := ginEngine(http.MethodPost, "/sign",
		Signature(SignatureConfig{
			Signer: HMACSHA256Signer(testSecret),
			Parts:  SignatureParts{Body: true},
		}),
		okHandler,
	)

	req := httptest.NewRequest(http.MethodPost, "/sign", strings.NewReader(body))
	req.Header.Set("X-Signature", sig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[Gin] status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSignature_Gin_Invalid(t *testing.T) {
	r := ginEngine(http.MethodPost, "/sign",
		Signature(SignatureConfig{
			Signer: HMACSHA256Signer(testSecret),
			Parts:  SignatureParts{Body: true},
		}),
		okHandler,
	)

	req := httptest.NewRequest(http.MethodPost, "/sign", strings.NewReader("body"))
	req.Header.Set("X-Signature", "tampered")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !containsString(w.Body.String(), `"code"`) {
		t.Errorf("[Gin] invalid signature should return error code, got: %s", w.Body.String())
	}
}
