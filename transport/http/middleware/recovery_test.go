package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// Recovery middleware tests.
// Verify consistent behavior with net/http and Gin.
// ============================================================================

func TestRecovery_NoPanic(t *testing.T) {
	mw := Recovery()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("all good"))
	})

	w := do(mw(inner), http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "all good" {
		t.Errorf("body = %q, want %q", w.Body.String(), "all good")
	}
}

func TestRecovery_PanicRecovers(t *testing.T) {
	mw := Recovery()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})

	w := do(mw(inner), http.MethodGet, "/", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("panic should result in 500, got %d", w.Code)
	}
}

func TestRecovery_PanicWithError(t *testing.T) {
	mw := Recovery()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	w := do(mw(inner), http.MethodGet, "/", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("panic(error) should result in 500, got %d", w.Code)
	}
}

func TestRecovery_NoStackTrace(t *testing.T) {
	// Recovery should preserve the response status when StackTrace is false.
	mw := Recovery(RecoveryConfig{StackTrace: false})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no stack")
	})

	w := do(mw(inner), http.MethodGet, "/", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestRecovery_PanicAfterWriteHeader(t *testing.T) {
	// Recovering after headers were sent must not panic.
	mw := Recovery()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic("panic after write")
	})

	// The handler must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Recovery middleware should have caught panic: %v", r)
		}
	}()
	do(mw(inner), http.MethodGet, "/", nil)
}

// ============================================================================
// Gin integration test.
// ============================================================================

func TestRecovery_Gin(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("gin panic test")
	})

	r := ginEngine(http.MethodGet, "/panic", Recovery(), inner)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("[Gin] panic should return 500, got %d", w.Code)
	}
}

func TestRecovery_Gin_NoPanic(t *testing.T) {
	r := ginEngine(http.MethodGet, "/ok", Recovery(), okHandler)

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[Gin] no panic should return 200, got %d", w.Code)
	}
}
