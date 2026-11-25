package http

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/cors"

	"github.com/kochabx/kit/cx"
	"github.com/kochabx/kit/fault"
)

func selfSignedTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
}

func TestRouterHandleEmptySlicePreservedAndValidation(t *testing.T) {
	type reqDTO struct {
		Name string `json:"name"`
	}
	type respDTO struct {
		Items []string `json:"items,omitzero"` // with slice nil vs empty []string{}
		Empty []string `json:"empty"`
	}

	rt := NewRouter(WithValidator(ValidatorFunc(func(_ context.Context, v any) error {
		if r, ok := v.(*reqDTO); ok && r.Name == "bad" {
			return fault.InvalidArgument("bad name")
		}
		return nil
	})))

	rt.Handle("POST /v1/orders", func(_ context.Context, _ reqDTO) (respDTO, error) {
		return respDTO{Items: []string{}, Empty: []string{}}, nil
	})
	rt.Handle("GET /v1/boom", func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, errors.New("secret-sql-error")
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/orders", strings.NewReader(`{"name":"ok"}`))
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"empty":[]`) {
		t.Fatalf("unexpected router output: code=%d body=%s", rec.Code, rec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/v1/orders", strings.NewReader(`{"name":"bad"}`))
	badRec := httptest.NewRecorder()
	rt.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest || !strings.Contains(badRec.Body.String(), "bad name") {
		t.Fatalf("expected 400 Problem, got code=%d body=%s", badRec.Code, badRec.Body.String())
	}

	boomReq := httptest.NewRequest(http.MethodGet, "/v1/boom", nil)
	boomRec := httptest.NewRecorder()
	rt.ServeHTTP(boomRec, boomReq)
	if boomRec.Code != http.StatusInternalServerError || strings.Contains(boomRec.Body.String(), "secret-sql-error") {
		t.Fatalf("internal error leaked: code=%d body=%s", boomRec.Code, boomRec.Body.String())
	}
}

func TestServerTLSWithoutFilesAndScalarContentTypes(t *testing.T) {
	tlsCfg := selfSignedTLSConfig(t)
	c := cx.New()
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Stop(t.Context()) }()

	srv := NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			WriteOK(w, map[string]string{"ok": "yes"})
		}),
		WithAddr("127.0.0.1:0"),
		WithTLSConfig(tlsCfg),
		WithHealthEndpoint("/healthz", c),
		WithOpenAPI(OpenAPIConfig{
			UIPath:    "/docs",
			SpecPath:  "/openapi.yaml",
			Spec:      []byte("openapi: 3.1.0\ninfo:\n  title: Demo\n  version: 1.0.0\n"),
			ScalarSRI: "sha384-test",
		}),
	)
	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("TLS Start failed: %v", err)
	}
	defer func() { _ = srv.Stop(t.Context()) }()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	defer client.CloseIdleConnections()

	// Must serve HTTPS (not downgraded to plain HTTP) when only TLSConfig is provided.
	resp, err := client.Get("https://" + srv.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("HTTPS request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	yamlResp, err := client.Get("https://" + srv.Addr() + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	yamlResp.Body.Close()
	if ct := yamlResp.Header.Get("Content-Type"); !strings.Contains(ct, "application/yaml") {
		t.Fatalf("YAML spec Content-Type = %q, want application/yaml", ct)
	}

	uiResp, err := client.Get("https://" + srv.Addr() + "/docs/")
	if err != nil {
		t.Fatal(err)
	}
	uiBody, _ := io.ReadAll(uiResp.Body)
	uiResp.Body.Close()
	if !strings.Contains(string(uiBody), `@scalar/api-reference@1.28.30`) || !strings.Contains(string(uiBody), `integrity="sha384-test"`) {
		t.Fatalf("Scalar UI missing pinned version or SRI: %s", uiBody)
	}
}

func TestHardenedMiddlewaresAndReplayCache(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	csrfMW, err := CSRF("https://trusted.example")
	if err != nil {
		t.Fatal(err)
	}
	corsMW := CORS(cors.Options{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowCredentials: true,
	})
	replay := NewMemoryReplayStore(100)

	chain := Recovery(logger)(Logger(logger)(corsMW(csrfMW(
		Auth(AuthenticatorFunc(func(ctx context.Context, r *http.Request) (context.Context, error) {
			if r.Header.Get("Authorization") != "Bearer good" {
				return ctx, errors.New("missing token")
			}
			return ctx, nil
		}))(
			Signature(SignatureConfig{
				ReplayStore: replay,
				Verifier: SignatureVerifierFunc(func(_ context.Context, r *http.Request, body []byte) (string, time.Duration, error) {
					return r.Header.Get("X-Nonce"), time.Minute, nil
				}),
			})(
				Crypto(DecryptorFunc(func(_ context.Context, ct []byte) ([]byte, error) {
					return bytes.TrimPrefix(ct, []byte("enc:")), nil
				}), 1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/abort" {
						panic(http.ErrAbortHandler)
					}
					if r.URL.Path == "/panic" {
						panic("boom")
					}
					b, _ := io.ReadAll(r.Body)
					WriteOK(w, string(b))
				})),
			),
		),
	))))

	// 1. CORS must NOT reflect evil domain bypassing suffix check.
	evilReq := httptest.NewRequest(http.MethodGet, "/ok", nil)
	evilReq.Header.Set("Origin", "https://evil.com/.example.com")
	evilRec := httptest.NewRecorder()
	chain.ServeHTTP(evilRec, evilReq)
	if evilRec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CORS reflected untrusted origin: %q", evilRec.Header().Get("Access-Control-Allow-Origin"))
	}

	// 2. CSRF rejects cross-origin unsafe POST.
	csrfReq := httptest.NewRequest(http.MethodPost, "/ok", nil)
	csrfReq.Header.Set("Sec-Fetch-Site", "cross-site")
	csrfRec := httptest.NewRecorder()
	chain.ServeHTTP(csrfRec, csrfReq)
	if csrfRec.Code != http.StatusForbidden {
		t.Fatalf("CSRF status = %d, want 403", csrfRec.Code)
	}

	// 3. Valid decrypt + replay protection.
	makeSignedReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/ok", strings.NewReader("enc:hello-world"))
		r.Header.Set("Authorization", "Bearer good")
		r.Header.Set("X-Nonce", "nonce-1")
		return r
	}
	rec1 := httptest.NewRecorder()
	chain.ServeHTTP(rec1, makeSignedReq())
	var env Envelope[string]
	if err := json.Unmarshal(rec1.Body.Bytes(), &env); err != nil || rec1.Code != http.StatusOK || env.Data != "hello-world" {
		t.Fatalf("decrypted response = code:%d body:%s (%v)", rec1.Code, rec1.Body.String(), err)
	}
	rec2 := httptest.NewRecorder()
	chain.ServeHTTP(rec2, makeSignedReq())
	if rec2.Code != http.StatusBadRequest || !strings.Contains(rec2.Body.String(), "replay") {
		t.Fatalf("expected replay rejection, got code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 4. Recovery catches normal panic AND masks Authorization header.
	panicReq := httptest.NewRequest(http.MethodGet, "/panic", nil)
	panicReq.Header.Set("Authorization", "Bearer good")
	panicReq.Header.Set("X-Nonce", "nonce-2")
	panicRec := httptest.NewRecorder()
	chain.ServeHTTP(panicRec, panicReq)
	if panicRec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want 500", panicRec.Code)
	}
	if strings.Contains(logBuf.String(), "Bearer good") {
		t.Fatalf("Recovery leaked Authorization header in logs: %s", logBuf.String())
	}

	// 5. Recovery must re-panic http.ErrAbortHandler.
	abortReq := httptest.NewRequest(http.MethodGet, "/abort", nil)
	abortReq.Header.Set("Authorization", "Bearer good")
	abortReq.Header.Set("X-Nonce", "nonce-3")
	func() {
		defer func() {
			if r := recover(); r != http.ErrAbortHandler {
				t.Fatalf("expected http.ErrAbortHandler re-panic, got %v", r)
			}
		}()
		chain.ServeHTTP(httptest.NewRecorder(), abortReq)
	}()

	// 6. Path traversal under SkipConfig prefix must NOT bypass Auth middleware.
	matcher := NewPathMatcher([]string{"/public/**"})
	authOnly := Auth(AuthenticatorFunc(func(ctx context.Context, r *http.Request) (context.Context, error) {
		return ctx, fault.Unauthorized("auth required")
	}), SkipConfig{Paths: []string{"/public/**"}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	if !matcher.Match("/public/docs") {
		t.Fatal("expected /public/docs to match")
	}
	travRec := httptest.NewRecorder()
	authOnly.ServeHTTP(travRec, httptest.NewRequest(http.MethodGet, "/public/../admin/secret", nil))
	if travRec.Code != http.StatusUnauthorized {
		t.Fatalf("path traversal bypassed Auth middleware: status=%d", travRec.Code)
	}
}

func writeTestKeyPair(t *testing.T, dir, prefix string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, prefix+".crt")
	keyPath := filepath.Join(dir, prefix+".key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestServerTLSCertFilesValidationAndSyncErrors(t *testing.T) {
	dir := t.TempDir()
	certA, keyA := writeTestKeyPair(t, dir, "a")
	_, keyB := writeTestKeyPair(t, dir, "b")

	for _, tc := range []struct {
		name     string
		certFile string
		keyFile  string
	}{
		{"missing key file", certA, ""},
		{"missing cert file", "", keyA},
		{"nonexistent files", filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key")},
		{"mismatched cert and key", certA, keyB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(nil, WithAddr("127.0.0.1:0"), WithTLSCertFiles(tc.certFile, tc.keyFile))
			if err := srv.Start(t.Context()); err == nil {
				_ = srv.Stop(t.Context())
				t.Fatalf("expected Start error for %s, got nil", tc.name)
			}
			if srv.Addr() != "127.0.0.1:0" {
				t.Fatalf("listener should not have bound on failure, got addr %q", srv.Addr())
			}
			if err := srv.HealthCheck(t.Context()); err == nil {
				t.Fatal("HealthCheck must fail when Start failed")
			}
		})
	}

	// Valid cert/key pair succeeds and serves HTTPS.
	validSrv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteOK(w, "tls-ok")
	}), WithAddr("127.0.0.1:0"), WithTLSCertFiles(certA, keyA))
	if err := validSrv.Start(t.Context()); err != nil {
		t.Fatalf("valid TLS Start failed: %v", err)
	}
	defer func() { _ = validSrv.Stop(t.Context()) }()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	defer client.CloseIdleConnections()
	resp, err := client.Get("https://" + validSrv.Addr() + "/")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTPS GET = (%+v, %v)", resp, err)
	}
	resp.Body.Close()
}

func TestServerTLSConfigSourcesAndListenerCleanup(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeTestKeyPair(t, dir, "srv")
	keyPair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// Reserve a concrete address, release it, then fail Start on TLS config with no cert source.
	probeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probeLis.Addr().String()
	probeLis.Close()

	badSrv := NewServer(nil, WithAddr(addr), WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	if err := badSrv.Start(t.Context()); err == nil {
		_ = badSrv.Stop(t.Context())
		t.Fatal("expected Start error for TLSConfig without certificates or callbacks")
	}
	if err := badSrv.HealthCheck(t.Context()); err == nil {
		t.Fatal("HealthCheck must fail when Start rejected missing TLS certs")
	}
	rebindLis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("expected %s to remain unbound after failed TLS Start: %v", addr, err)
	}
	rebindLis.Close()

	// Both static Certificates and dynamic GetCertificate callback start and serve HTTPS.
	for _, tc := range []struct {
		name string
		cfg  *tls.Config
	}{
		{
			name: "static Certificates",
			cfg:  &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{keyPair}},
		},
		{
			name: "GetCertificate callback",
			cfg: &tls.Config{
				MinVersion: tls.VersionTLS12,
				GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					return &keyPair, nil
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				WriteOK(w, tc.name)
			}), WithAddr("127.0.0.1:0"), WithTLSConfig(tc.cfg))
			if err := srv.Start(t.Context()); err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			defer func() {
				if err := srv.Stop(t.Context()); err != nil {
					t.Fatalf("Stop failed: %v", err)
				}
			}()

			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
			defer client.CloseIdleConnections()
			resp, err := client.Get("https://" + srv.Addr() + "/")
			if err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("HTTPS GET = (%+v, %v)", resp, err)
			}
			resp.Body.Close()
		})
	}
}

func TestMemoryReplayStoreStrictTTLAndCapacityRejection(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	store := NewMemoryReplayStore(2, func() time.Time { return now })

	if firstUse, err := store.Use(t.Context(), "n1", 10*time.Second); err != nil || !firstUse {
		t.Fatalf("n1 first use = (%v, %v), want (true, nil)", firstUse, err)
	}
	if firstUse, err := store.Use(t.Context(), "n2", 20*time.Second); err != nil || !firstUse {
		t.Fatalf("n2 first use = (%v, %v), want (true, nil)", firstUse, err)
	}

	// Capacity full of unexpired keys: reject n3 via Signature middleware with 503 while preserving n1 and n2.
	mw := Signature(SignatureConfig{
		ReplayStore: store,
		Verifier: SignatureVerifierFunc(func(_ context.Context, r *http.Request, _ []byte) (string, time.Duration, error) {
			return r.Header.Get("X-Nonce"), 10 * time.Second, nil
		}),
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteOK(w, "ok")
	}))

	reqN3 := httptest.NewRequest(http.MethodPost, "/signed", nil)
	reqN3.Header.Set("X-Nonce", "n3")
	recN3 := httptest.NewRecorder()
	mw.ServeHTTP(recN3, reqN3)
	if recN3.Code != http.StatusServiceUnavailable {
		t.Fatalf("status while replay store full = %d, want 503 (%s)", recN3.Code, recN3.Body.String())
	}

	// n1 and n2 remain rejected as replays inside their TTL.
	for _, k := range []string{"n1", "n2"} {
		firstUse, err := store.Use(t.Context(), k, 10*time.Second)
		if err != nil || firstUse {
			t.Fatalf("live key %q must remain replay (firstUse=false): (%v, %v)", k, firstUse, err)
		}
	}

	// Advance past n1's 10s TTL: n1 expires, n2 stays active, and n3 succeeds once then replays with 400.
	now = now.Add(10 * time.Second)
	if firstUse, err := store.Use(t.Context(), "n2", 10*time.Second); err != nil || firstUse {
		t.Fatalf("live key n2 must remain replay at +10s: (%v, %v)", firstUse, err)
	}
	recN3OK := httptest.NewRecorder()
	mw.ServeHTTP(recN3OK, reqN3)
	if recN3OK.Code != http.StatusOK {
		t.Fatalf("n3 after n1 expiry = %d, want 200 (%s)", recN3OK.Code, recN3OK.Body.String())
	}
	recN3Replay := httptest.NewRecorder()
	mw.ServeHTTP(recN3Replay, reqN3)
	if recN3Replay.Code != http.StatusBadRequest {
		t.Fatalf("n3 replay = %d, want 400 (%s)", recN3Replay.Code, recN3Replay.Body.String())
	}
}

func TestServerStopTimeoutRetryUntilDrained(t *testing.T) {
	inFlight := make(chan struct{})
	release := make(chan struct{})
	srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		<-release
		w.WriteHeader(http.StatusOK)
	}), WithAddr("127.0.0.1:0"))

	if err := srv.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	reqDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr() + "/slow")
		if resp != nil {
			resp.Body.Close()
		}
		reqDone <- err
	}()
	<-inFlight

	ctx1, cancel1 := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel1()
	if err := srv.Stop(ctx1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Stop = %v, want DeadlineExceeded", err)
	}
	if err := srv.HealthCheck(t.Context()); err == nil {
		t.Fatal("HealthCheck must fail while draining")
	}

	// Second Stop while request is still blocked must NOT falsely succeed.
	ctx2, cancel2 := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel2()
	if err := srv.Stop(ctx2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Stop while still blocked = %v, want DeadlineExceeded", err)
	}

	close(release)
	if err := <-reqDone; err != nil {
		t.Fatalf("in-flight HTTP request failed: %v", err)
	}
	if err := srv.Stop(t.Context()); err != nil {
		t.Fatalf("third Stop after handler release failed: %v", err)
	}
	if err := srv.Stop(t.Context()); err != nil {
		t.Fatalf("idempotent Stop after idle failed: %v", err)
	}
	if err := srv.Start(t.Context()); err == nil {
		_ = srv.Stop(t.Context())
		t.Fatal("expected error when restarting stopped HTTP server")
	}
	if err := srv.HealthCheck(t.Context()); err == nil {
		t.Fatal("HealthCheck must fail after Stop")
	}
}
