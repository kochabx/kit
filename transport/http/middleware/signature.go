package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	corehmac "github.com/kochabx/kit/core/crypto/hmac"
	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	kithttp "github.com/kochabx/kit/transport/http"
)

const (
	DefaultSignatureHeader = "X-Signature"
	DefaultMaxSignedBody   = 1 << 20
)

var (
	ErrSignatureFailed = errors.BadRequest("verify signature failed")
	ErrSignatureReplay = errors.BadRequest("signature replay detected")
)

// ReplayStore atomically records a nonce for ttl. used is true when the nonce
// was already recorded. Distributed deployments should use a shared store.
type ReplayStore interface {
	Use(ctx context.Context, key string, ttl time.Duration) (used bool, err error)
}

// MemoryReplayStore is suitable for one process and tests. It is not shared
// across replicas; distributed services should provide a Redis-backed store.
type MemoryReplayStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	clock   func() time.Time
}

func NewMemoryReplayStore() *MemoryReplayStore {
	return &MemoryReplayStore{entries: make(map[string]time.Time), clock: time.Now}
}

func (store *MemoryReplayStore) Use(_ context.Context, key string, ttl time.Duration) (bool, error) {
	if store == nil || key == "" || ttl <= 0 {
		return false, fmt.Errorf("signature: invalid replay store input")
	}
	now := store.clock()
	store.mu.Lock()
	defer store.mu.Unlock()
	if expiresAt, ok := store.entries[key]; ok && now.Before(expiresAt) {
		return true, nil
	}
	store.entries[key] = now.Add(ttl)
	for existingKey, expiresAt := range store.entries {
		if !now.Before(expiresAt) {
			delete(store.entries, existingKey)
		}
	}
	return false, nil
}

type signatureConfig struct {
	Skip           SkipConfig
	Signer         *corehmac.Signer
	ReplayStore    ReplayStore
	HeaderName     string
	MaxBodyBytes   int64
	SuccessHandler func(http.ResponseWriter, *http.Request)
	ErrorHandler   func(http.ResponseWriter, *http.Request, error)
	Logger         *log.Logger
}

type SignatureOption func(*signatureConfig)

func WithSignatureReplayStore(store ReplayStore) SignatureOption {
	return func(config *signatureConfig) { config.ReplayStore = store }
}

func WithSignatureHeader(name string) SignatureOption {
	return func(config *signatureConfig) { config.HeaderName = name }
}

func WithSignatureMaxBodyBytes(size int64) SignatureOption {
	return func(config *signatureConfig) { config.MaxBodyBytes = size }
}

func WithSignatureSkip(skip SkipConfig) SignatureOption {
	return func(config *signatureConfig) { config.Skip = skip }
}

func WithSignatureErrorHandler(handler func(http.ResponseWriter, *http.Request, error)) SignatureOption {
	return func(config *signatureConfig) { config.ErrorHandler = handler }
}

func WithSignatureSuccessHandler(handler func(http.ResponseWriter, *http.Request)) SignatureOption {
	return func(config *signatureConfig) { config.SuccessHandler = handler }
}

func WithSignatureLogger(logger *log.Logger) SignatureOption {
	return func(config *signatureConfig) { config.Logger = logger }
}

// SignRequest signs request's canonical method, host, escaped path, complete
// query, content type and body digest, then writes the signature header. The
// request body remains readable.
func SignRequest(request *http.Request, signer *corehmac.Signer, headerNames ...string) error {
	if request == nil || signer == nil {
		return fmt.Errorf("signature: request and signer are required")
	}
	payload, err := canonicalRequest(request, 0)
	if err != nil {
		return err
	}
	signature, err := signer.Sign(payload)
	if err != nil {
		return err
	}
	encoded, err := encodeSignature(signature)
	if err != nil {
		return err
	}
	headerName := DefaultSignatureHeader
	if len(headerNames) > 0 && headerNames[0] != "" {
		headerName = headerNames[0]
	}
	request.Header.Set(headerName, encoded)
	return nil
}

// Signature verifies a signed request and rejects a nonce reused within the
// signature validity window.
func Signature(signer *corehmac.Signer, options ...SignatureOption) func(http.Handler) http.Handler {
	if signer == nil {
		panic("middleware: signature signer is required")
	}
	config := signatureConfig{Signer: signer, ReplayStore: NewMemoryReplayStore()}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	if config.ReplayStore == nil {
		panic("middleware: signature replay store is required")
	}
	if config.HeaderName == "" {
		config.HeaderName = DefaultSignatureHeader
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = DefaultMaxSignedBody
	}
	if config.Logger == nil {
		config.Logger = log.Global()
	}
	if config.ErrorHandler == nil {
		config.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
			kithttp.Fail(w, http.StatusBadRequest, http.StatusBadRequest, ErrSignatureFailed)
		}
	}
	matcher := NewPathMatcher(config.Skip.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if shouldSkip(request, matcher, config.Skip.Func) {
				next.ServeHTTP(w, request)
				return
			}
			encoded := request.Header.Get(config.HeaderName)
			signature, err := decodeSignature(encoded)
			if err != nil {
				config.Logger.Error().Err(err).Msg("signature: invalid header")
				config.ErrorHandler(w, request, err)
				return
			}
			payload, err := canonicalRequest(request, config.MaxBodyBytes)
			if err != nil {
				config.Logger.Error().Err(err).Msg("signature: canonicalize request failed")
				config.ErrorHandler(w, request, err)
				return
			}
			if err := config.Signer.Verify(signature, payload); err != nil {
				config.Logger.Error().Err(err).Msg("signature: verification failed")
				config.ErrorHandler(w, request, err)
				return
			}
			used, err := config.ReplayStore.Use(request.Context(), signature.ReplayKey(), config.Signer.Expiration())
			if err != nil || used {
				if used {
					err = ErrSignatureReplay
				}
				config.Logger.Error().Err(err).Msg("signature: replay check failed")
				config.ErrorHandler(w, request, err)
				return
			}
			if config.SuccessHandler != nil {
				config.SuccessHandler(w, request)
			}
			next.ServeHTTP(w, request)
		})
	}
}

func encodeSignature(signature corehmac.Signature) (string, error) {
	encoded, err := json.Marshal(signature)
	if err != nil {
		return "", fmt.Errorf("signature: encode: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeSignature(encoded string) (corehmac.Signature, error) {
	if encoded == "" {
		return corehmac.Signature{}, fmt.Errorf("signature: header is empty")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return corehmac.Signature{}, fmt.Errorf("signature: decode header: %w", err)
	}
	var signature corehmac.Signature
	if err := json.Unmarshal(data, &signature); err != nil {
		return corehmac.Signature{}, fmt.Errorf("signature: decode JSON: %w", err)
	}
	return signature, nil
}

func canonicalRequest(request *http.Request, maxBodyBytes int64) ([]byte, error) {
	body, err := readAndRestoreBody(request, maxBodyBytes)
	if err != nil {
		return nil, err
	}
	bodyDigest := sha256.Sum256(body)
	query := canonicalQuery(request.URL.Query())
	fields := [][]byte{
		[]byte(request.Method),
		[]byte(request.Host),
		[]byte(request.URL.EscapedPath()),
		[]byte(query),
		[]byte(request.Header.Get("Content-Type")),
		bodyDigest[:],
	}
	var output bytes.Buffer
	output.WriteByte(1)
	var length [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		output.Write(length[:])
		output.Write(field)
	}
	return output.Bytes(), nil
}

func canonicalQuery(values url.Values) string {
	cloned := make(url.Values, len(values))
	for key, entries := range values {
		cloned[key] = append([]string(nil), entries...)
	}
	return cloned.Encode()
}

func readAndRestoreBody(request *http.Request, maxBodyBytes int64) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	reader := io.Reader(request.Body)
	if maxBodyBytes > 0 {
		reader = io.LimitReader(request.Body, maxBodyBytes+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("signature: read body: %w", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	if maxBodyBytes > 0 && int64(len(body)) > maxBodyBytes {
		return nil, fmt.Errorf("signature: body exceeds %d bytes", maxBodyBytes)
	}
	return body, nil
}
