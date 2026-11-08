package middleware

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"

	"github.com/kochabx/kit/core/crypto/ecies"
	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	kithttp "github.com/kochabx/kit/transport/http"
)

var (
	ErrDecryptFailed = errors.Internal("decrypt request body failed")
)

// Decryptor decrypts a request payload.
type Decryptor interface {
	Decrypt(ciphertext []byte) ([]byte, error)
}

// DecryptorFunc adapts a function to Decryptor.
type DecryptorFunc func(ciphertext []byte) ([]byte, error)

func (f DecryptorFunc) Decrypt(ciphertext []byte) ([]byte, error) {
	return f(ciphertext)
}

// CryptoConfig configures request decryption middleware.
type CryptoConfig struct {
	Skip           SkipConfig                                      // Skip rules.
	Decryptor      Decryptor                                       // Required decryptor.
	SuccessHandler func(http.ResponseWriter, *http.Request)        // Success callback.
	ErrorHandler   func(http.ResponseWriter, *http.Request, error) // Error handler.
	Logger         *log.Logger                                     // Custom logger.
}

// HPKEDecryptor creates a decryptor for JSON-encoded HPKE messages. The info
// value is protocol domain separation and must match the sender. Bind
// request-specific metadata with a custom Decryptor when AAD is required.
func HPKEDecryptor(suite ecies.Suite, privateKey *ecies.PrivateKey, info []byte) Decryptor {
	info = bytes.Clone(info)
	return DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
		var message ecies.Message
		if err := json.Unmarshal(ciphertext, &message); err != nil {
			return nil, err
		}
		return suite.Open(privateKey, info, nil, &message)
	})
}

// Crypto creates request body decryption middleware.
func Crypto(cfgs ...CryptoConfig) func(http.Handler) http.Handler {
	cfg := CryptoConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.Global()
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			kithttp.Fail(w, http.StatusBadRequest, http.StatusBadRequest, ErrDecryptFailed)
		}
	}

	matcher := NewPathMatcher(cfg.Skip.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.Skip.Func) {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				cfg.Logger.Error().Err(err).Msg("crypto: read request body failed")
				cfg.ErrorHandler(w, r, err)
				return
			}

			if len(body) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			plaintext, err := cfg.Decryptor.Decrypt(body)
			if err != nil {
				cfg.Logger.Error().Err(err).Msg("crypto: decrypt failed")
				cfg.ErrorHandler(w, r, err)
				return
			}

			r.Body = io.NopCloser(bytes.NewBuffer(plaintext))
			r.ContentLength = int64(len(plaintext))

			if cfg.SuccessHandler != nil {
				cfg.SuccessHandler(w, r)
			}
			next.ServeHTTP(w, r)
		})
	}
}
