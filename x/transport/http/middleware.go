package http

import (
	"bytes"
	"cmp"
	"container/heap"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/cors"

	"github.com/kochabx/kit/fault"
)

type prefixRule struct {
	prefix string
	length int
}

// PathMatcher matches exact paths, prefix wildcards ("/api/**"), and path.Match globs.
type PathMatcher struct {
	exact    map[string]struct{}
	prefixes []prefixRule
	globs    []string
}

func NewPathMatcher(paths []string) *PathMatcher {
	if len(paths) == 0 {
		return nil
	}
	pm := &PathMatcher{exact: make(map[string]struct{}, len(paths))}
	for _, p := range paths {
		if pfx, ok := strings.CutSuffix(p, "/**"); ok {
			pm.prefixes = append(pm.prefixes, prefixRule{prefix: pfx, length: len(pfx)})
		} else if strings.ContainsAny(p, "*?[") {
			pm.globs = append(pm.globs, p)
		} else {
			pm.exact[p] = struct{}{}
		}
	}
	return pm
}

func (pm *PathMatcher) Match(urlPath string) bool {
	if pm == nil {
		return false
	}
	if _, ok := pm.exact[urlPath]; ok {
		return true
	}
	for _, pr := range pm.prefixes {
		if len(urlPath) == pr.length && urlPath == pr.prefix {
			return true
		}
		if len(urlPath) > pr.length && urlPath[pr.length] == '/' && strings.HasPrefix(urlPath, pr.prefix) {
			return true
		}
	}
	for _, g := range pm.globs {
		if ok, _ := path.Match(g, urlPath); ok {
			return true
		}
	}
	return false
}

// SkipConfig configures path or predicate exemptions for middleware.
type SkipConfig struct {
	Paths []string
	Func  func(*http.Request) bool
}

func shouldSkip(r *http.Request, m *PathMatcher, fn func(*http.Request) bool) bool {
	if fn != nil && fn(r) {
		return true
	}
	if m == nil {
		return false
	}
	for seg := range strings.SplitSeq(r.URL.Path, "/") {
		if seg == ".." {
			return false
		}
	}
	return m.Match(path.Clean(r.URL.Path))
}

// ResponseRecorder wraps http.ResponseWriter with status tracking and Unwrap() for http.ResponseController.
type ResponseRecorder struct {
	http.ResponseWriter
	status  int
	bytes   int
	written bool
}

func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *ResponseRecorder) Status() int                 { return r.status }
func (r *ResponseRecorder) BytesWritten() int           { return r.bytes }
func (r *ResponseRecorder) Written() bool               { return r.written }
func (r *ResponseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *ResponseRecorder) WriteHeader(code int) {
	if code >= http.StatusContinue && code <= 199 && code != http.StatusSwitchingProtocols {
		r.ResponseWriter.WriteHeader(code)
		return
	}
	if !r.written {
		r.status = code
		r.written = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *ResponseRecorder) Write(p []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// Logger logs structured HTTP access entries via *slog.Logger using LogAttrs.
func Logger(logger *slog.Logger, skip ...SkipConfig) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	var sc SkipConfig
	if len(skip) > 0 {
		sc = skip[0]
	}
	matcher := NewPathMatcher(sc.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, sc.Func) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rec := NewResponseRecorder(w)
			next.ServeHTTP(rec, r)

			level := slog.LevelInfo
			if rec.Status() >= 500 {
				level = slog.LevelError
			} else if rec.Status() >= 400 {
				level = slog.LevelWarn
			}
			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.Status()),
				slog.Int("bytes", rec.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

var sensitiveHeaders = map[string]struct{}{
	"Authorization":       {},
	"Proxy-Authorization": {},
	"Cookie":              {},
	"Set-Cookie":          {},
	"X-Api-Key":           {},
	"X-Auth-Token":        {},
}

// RedactHeaders returns header metadata with sensitive credential values masked.
func RedactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vals := range h {
		canonical := http.CanonicalHeaderKey(k)
		if _, secret := sensitiveHeaders[canonical]; secret {
			out[canonical] = "[REDACTED]"
		} else {
			out[canonical] = strings.Join(vals, ", ")
		}
	}
	return out
}

// Recovery catches panics while strictly preserving http.ErrAbortHandler, redacting
// sensitive headers, and treating broken-pipe/conn-reset as client disconnects.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := NewResponseRecorder(w)
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok {
					if errors.Is(err, http.ErrAbortHandler) {
						panic(http.ErrAbortHandler)
					}
					if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
						logger.WarnContext(r.Context(), "http client disconnected",
							slog.String("method", r.Method),
							slog.String("path", r.URL.Path),
							slog.Any("error", err),
						)
						return
					}
				}
				logger.ErrorContext(r.Context(), "http panic recovered",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("headers", RedactHeaders(r.Header)),
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
				)
				if !rec.Written() {
					Fail(rec, r, fault.Internal("internal server error"))
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// CORS configures origin policy backed by github.com/rs/cors.
func CORS(opts ...cors.Options) func(http.Handler) http.Handler {
	var cfg cors.Options
	if len(opts) > 0 {
		cfg = opts[0]
	}
	return cors.New(cfg).Handler
}

// CSRF applies net/http.CrossOriginProtection with RFC 9457 error responses.
func CSRF(trustedOrigins ...string) (func(http.Handler) http.Handler, error) {
	p := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if origin == "" {
			continue
		}
		if err := p.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
	}
	p.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Fail(w, r, fault.Forbidden("cross-origin request denied"))
	}))
	return p.Handler, nil
}

// Authenticator authenticates an HTTP request and enriches its context.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (context.Context, error)
}

type AuthenticatorFunc func(context.Context, *http.Request) (context.Context, error)

func (f AuthenticatorFunc) Authenticate(ctx context.Context, r *http.Request) (context.Context, error) {
	return f(ctx, r)
}

// Auth enforces authentication via auth, skipping paths matching skip.
func Auth(auth Authenticator, skip ...SkipConfig) func(http.Handler) http.Handler {
	var sc SkipConfig
	if len(skip) > 0 {
		sc = skip[0]
	}
	matcher := NewPathMatcher(sc.Paths)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, sc.Func) {
				next.ServeHTTP(w, r)
				return
			}
			if auth == nil {
				Fail(w, r, fault.Internal("authenticator not configured"))
				return
			}
			ctx, err := auth.Authenticate(r.Context(), r)
			if err != nil {
				if _, isFault := fault.From(err); isFault {
					Fail(w, r, err)
				} else {
					Fail(w, r, fault.Unauthorized("unauthorized").WithCause(err))
				}
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Authorizer validates whether an already-authenticated request may proceed.
type Authorizer interface {
	Authorize(ctx context.Context, r *http.Request) error
}

type AuthorizerFunc func(context.Context, *http.Request) error

func (f AuthorizerFunc) Authorize(ctx context.Context, r *http.Request) error { return f(ctx, r) }

// Permission enforces authorization via authz, skipping paths matching skip.
func Permission(authz Authorizer, skip ...SkipConfig) func(http.Handler) http.Handler {
	var sc SkipConfig
	if len(skip) > 0 {
		sc = skip[0]
	}
	matcher := NewPathMatcher(sc.Paths)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, sc.Func) {
				next.ServeHTTP(w, r)
				return
			}
			if authz == nil {
				Fail(w, r, fault.Internal("authorizer not configured"))
				return
			}
			if err := authz.Authorize(r.Context(), r); err != nil {
				if _, isFault := fault.From(err); isFault {
					Fail(w, r, err)
				} else {
					Fail(w, r, fault.Forbidden("forbidden").WithCause(err))
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ErrReplayStoreFull indicates the bounded in-memory replay store is full of active keys.
var ErrReplayStoreFull = fault.ServiceUnavailable("http: replay store capacity exhausted")

// ReplayStore prevents reuse of replay keys across calls.
// Use records key for ttl and returns firstUse=true on first observation (allowed)
// or firstUse=false when key was already recorded within ttl (replay).
type ReplayStore interface {
	Use(ctx context.Context, key string, ttl time.Duration) (firstUse bool, err error)
}

type replayHeapItem struct {
	key       string
	expiresAt time.Time
}

type replayHeap []replayHeapItem

func (h replayHeap) Len() int           { return len(h) }
func (h replayHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h replayHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *replayHeap) Push(x any)        { *h = append(*h, x.(replayHeapItem)) }
func (h *replayHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = replayHeapItem{}
	*h = old[:n-1]
	return item
}

// MemoryReplayStore provides bounded, strict-TTL replay protection that never evicts live entries.
type MemoryReplayStore struct {
	mu      sync.Mutex
	maxCap  uint64
	clock   func() time.Time
	entries map[string]time.Time
	expiry  replayHeap
}

func NewMemoryReplayStore(maxEntries uint64, clock ...func() time.Time) *MemoryReplayStore {
	clk := time.Now
	if len(clock) > 0 && clock[0] != nil {
		clk = clock[0]
	}
	return &MemoryReplayStore{
		maxCap:  cmp.Or(maxEntries, uint64(100_000)),
		clock:   clk,
		entries: make(map[string]time.Time),
	}
}

func (s *MemoryReplayStore) Use(_ context.Context, key string, ttl time.Duration) (bool, error) {
	if s == nil || key == "" || ttl <= 0 {
		return false, fault.InvalidArgument("http: invalid replay store input")
	}

	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.expiry) > 0 && !now.Before(s.expiry[0].expiresAt) {
		item := heap.Pop(&s.expiry).(replayHeapItem)
		if cur, ok := s.entries[item.key]; ok && cur.Equal(item.expiresAt) {
			delete(s.entries, item.key)
		}
	}

	if _, ok := s.entries[key]; ok {
		return false, nil
	}
	if uint64(len(s.entries)) >= s.maxCap {
		return false, ErrReplayStoreFull
	}

	exp := now.Add(ttl)
	s.entries[key] = exp
	heap.Push(&s.expiry, replayHeapItem{key: key, expiresAt: exp})
	return true, nil
}

// SignatureVerifier verifies signed HTTP requests (decoupled from x/crypto).
type SignatureVerifier interface {
	Verify(ctx context.Context, r *http.Request, body []byte) (replayKey string, replayTTL time.Duration, err error)
}

type SignatureVerifierFunc func(context.Context, *http.Request, []byte) (string, time.Duration, error)

func (f SignatureVerifierFunc) Verify(ctx context.Context, r *http.Request, body []byte) (string, time.Duration, error) {
	return f(ctx, r, body)
}

// SignatureConfig configures request signature & anti-replay verification.
type SignatureConfig struct {
	Skip         SkipConfig
	Verifier     SignatureVerifier
	ReplayStore  ReplayStore
	MaxBodyBytes int64
}

// Signature verifies request signatures and enforces replay protection via ReplayStore.
func Signature(cfg SignatureConfig) func(http.Handler) http.Handler {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	matcher := NewPathMatcher(cfg.Skip.Paths)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.Skip.Func) {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.Verifier == nil {
				Fail(w, r, fault.Internal("signature verifier not configured"))
				return
			}
			var body []byte
			if r.Body != nil && r.Body != http.NoBody {
				var err error
				body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes))
				if err != nil {
					if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
						Fail(w, r, fault.BadRequest("request body too large").WithStatus(http.StatusRequestEntityTooLarge).WithCause(err))
						return
					}
					Fail(w, r, fault.BadRequest("read request body failed").WithCause(err))
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			}

			replayKey, replayTTL, err := cfg.Verifier.Verify(r.Context(), r, body)
			if err != nil {
				if _, isFault := fault.From(err); isFault {
					Fail(w, r, err)
				} else {
					Fail(w, r, fault.BadRequest("invalid signature").WithCause(err))
				}
				return
			}
			if cfg.ReplayStore != nil && replayKey != "" && replayTTL > 0 {
				firstUse, err := cfg.ReplayStore.Use(r.Context(), replayKey, replayTTL)
				if err != nil {
					if _, ok := fault.From(err); ok {
						Fail(w, r, err)
						return
					}
					Fail(w, r, fault.Internal("replay check failed").WithCause(err))
					return
				}
				if !firstUse {
					Fail(w, r, fault.BadRequest("signature replay detected"))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Decryptor decrypts HTTP request payloads (decoupled from x/crypto).
type Decryptor interface {
	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)
}

type DecryptorFunc func(context.Context, []byte) ([]byte, error)

func (f DecryptorFunc) Decrypt(ctx context.Context, ct []byte) ([]byte, error) { return f(ctx, ct) }

// Crypto decrypts non-empty request bodies and restores r.Body / Content-Length.
func Crypto(dec Decryptor, maxBodyBytes int64, skip ...SkipConfig) func(http.Handler) http.Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 1 << 20
	}
	var sc SkipConfig
	if len(skip) > 0 {
		sc = skip[0]
	}
	matcher := NewPathMatcher(sc.Paths)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, sc.Func) {
				next.ServeHTTP(w, r)
				return
			}
			if dec == nil {
				Fail(w, r, fault.Internal("decryptor not configured"))
				return
			}
			if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
				next.ServeHTTP(w, r)
				return
			}
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
			if err != nil {
				if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
					Fail(w, r, fault.BadRequest("request body too large").WithStatus(http.StatusRequestEntityTooLarge).WithCause(err))
					return
				}
				Fail(w, r, fault.BadRequest("read encrypted body failed").WithCause(err))
				return
			}
			if len(raw) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			plain, err := dec.Decrypt(r.Context(), raw)
			if err != nil {
				if _, isFault := fault.From(err); isFault {
					Fail(w, r, err)
				} else {
					Fail(w, r, fault.BadRequest("decrypt request body failed").WithCause(err))
				}
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(plain))
			r.ContentLength = int64(len(plain))
			r.Header.Set("Content-Length", strconv.Itoa(len(plain)))
			next.ServeHTTP(w, r)
		})
	}
}
