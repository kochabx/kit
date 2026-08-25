package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corehmac "github.com/kochabx/kit/core/crypto/hmac"
)

func testHMACSigner(t *testing.T) *corehmac.Signer {
	t.Helper()
	signer, err := corehmac.New(bytes.Repeat([]byte{0x42}, corehmac.MinKeySize))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestSignatureRoundTrip(t *testing.T) {
	signer := testHMACSigner(t)
	request := httptest.NewRequest(http.MethodPost, "https://api.example.test/orders?tag=b&tag=a", strings.NewReader(`{"amount":100}`))
	request.Header.Set("Content-Type", "application/json")
	if err := SignRequest(request, signer); err != nil {
		t.Fatal(err)
	}

	var body string
	handler := Signature(signer)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		data, _ := io.ReadAll(request.Body)
		body = string(data)
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	if body != `{"amount":100}` {
		t.Fatalf("body = %q", body)
	}
}

func TestSignatureRejectsTamperingAndReplay(t *testing.T) {
	signer := testHMACSigner(t)
	store := NewMemoryReplayStore()
	handler := Signature(signer, WithSignatureReplayStore(store))(okHandler)

	request := httptest.NewRequest(http.MethodPost, "https://api.example.test/orders?id=1", strings.NewReader("body"))
	if err := SignRequest(request, signer); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(request.Body)
	header := request.Header.Get(DefaultSignatureHeader)

	first := httptest.NewRequest(http.MethodPost, "https://api.example.test/orders?id=1", bytes.NewReader(body))
	first.Header.Set(DefaultSignatureHeader, header)
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d", firstRecorder.Code)
	}

	replay := httptest.NewRequest(http.MethodPost, "https://api.example.test/orders?id=1", bytes.NewReader(body))
	replay.Header.Set(DefaultSignatureHeader, header)
	replayRecorder := httptest.NewRecorder()
	handler.ServeHTTP(replayRecorder, replay)
	if replayRecorder.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d", replayRecorder.Code)
	}

	tampered := httptest.NewRequest(http.MethodPost, "https://api.example.test/orders?id=2", bytes.NewReader(body))
	tampered.Header.Set(DefaultSignatureHeader, header)
	tamperedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tamperedRecorder, tampered)
	if tamperedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("tampered status = %d", tamperedRecorder.Code)
	}
}

func TestCanonicalQueryPreservesRepeatedValueOrder(t *testing.T) {
	a := httptest.NewRequest(http.MethodGet, "https://example.test/?tag=b&tag=a", nil)
	b := httptest.NewRequest(http.MethodGet, "https://example.test/?tag=a&tag=b", nil)
	aData, err := canonicalRequest(a, DefaultMaxSignedBody)
	if err != nil {
		t.Fatal(err)
	}
	bData, err := canonicalRequest(b, DefaultMaxSignedBody)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(aData, bData) {
		t.Fatal("different repeated-value order has the same canonical form")
	}
}

func TestSignatureRejectsOversizedBody(t *testing.T) {
	signer := testHMACSigner(t)
	request := httptest.NewRequest(http.MethodPost, "https://example.test/upload", strings.NewReader("too large"))
	if err := SignRequest(request, signer); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler := Signature(signer, WithSignatureMaxBodyBytes(3))(okHandler)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}
