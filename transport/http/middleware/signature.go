package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	kithttp "github.com/kochabx/kit/transport/http"
)

var (
	ErrSignatureFailed = errors.BadRequest("verify signature failed")
)

// Signer 签名验证器接口
type Signer interface {
	Verify(data []byte, signature string) error
}

// SignerFunc 签名验证器函数适配器
type SignerFunc func(data []byte, signature string) error

func (f SignerFunc) Verify(data []byte, signature string) error {
	return f(data, signature)
}

// HMACSHA256Signer 创建 HMAC-SHA256 签名验证器
func HMACSHA256Signer(secret string) Signer {
	return SignerFunc(func(data []byte, signature string) error {
		h := hmac.New(sha256.New, []byte(secret))
		h.Write(data)
		expected := base64.StdEncoding.EncodeToString(h.Sum(nil))
		if expected != signature {
			return ErrSignatureFailed
		}
		return nil
	})
}

// SignatureParts 控制签名数据中包含的请求组成部分
type SignatureParts struct {
	Params bool // 是否包含 Query 参数
	Body   bool // 是否包含请求体
	Path   bool // 是否包含请求路径
	Method bool // 是否包含请求方法
}

// SignatureConfig 签名验证中间件配置
type SignatureConfig struct {
	Skip           SkipConfig                                      // 跳过配置
	Parts          SignatureParts                                  // 签名数据组成部分
	Signer         Signer                                          // 签名验证器（必需）
	HeaderName     string                                          // 签名头名称，默认 "X-Signature"
	SuccessHandler func(http.ResponseWriter, *http.Request)        // 成功回调
	ErrorHandler   func(http.ResponseWriter, *http.Request, error) // 错误处理函数
	Logger         *log.Logger                                     // 自定义日志记录器
}

// DefaultSignatureConfig 返回默认签名配置
func DefaultSignatureConfig() SignatureConfig {
	return SignatureConfig{
		HeaderName: "X-Signature",
		Parts: SignatureParts{
			Params: true,
			Body:   true,
		},
	}
}

// Signature 创建请求签名验证中间件
func Signature(cfgs ...SignatureConfig) func(http.Handler) http.Handler {
	cfg := SignatureConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	if cfg.Signer == nil {
		panic("middleware: Signer is required")
	}

	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-Signature"
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			kithttp.Fail(w, http.StatusBadRequest, ErrSignatureFailed)
		}
	}

	matcher := NewPathMatcher(cfg.Skip.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.Skip.Func) {
				next.ServeHTTP(w, r)
				return
			}

			signature := r.Header.Get(cfg.HeaderName)
			if signature == "" {
				cfg.Logger.Error().Str("header", cfg.HeaderName).Msg("signature: header missing")
				cfg.ErrorHandler(w, r, ErrSignatureFailed)
				return
			}

			data, err := buildSignatureData(r, cfg)
			if err != nil {
				cfg.Logger.Error().Err(err).Msg("signature: build data failed")
				cfg.ErrorHandler(w, r, err)
				return
			}

			if err := cfg.Signer.Verify(data, signature); err != nil {
				cfg.Logger.Error().Err(err).Msg("signature: verify failed")
				cfg.ErrorHandler(w, r, err)
				return
			}

			if cfg.SuccessHandler != nil {
				cfg.SuccessHandler(w, r)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// buildSignatureData 构建待签名数据
func buildSignatureData(r *http.Request, cfg SignatureConfig) ([]byte, error) {
	var builder strings.Builder

	if cfg.Parts.Method {
		builder.WriteString(r.Method)
	}

	if cfg.Parts.Path {
		builder.WriteString(r.URL.Path)
	}

	if cfg.Parts.Params {
		params := r.URL.Query()
		if len(params) > 0 {
			keys := make([]string, 0, len(params))
			for k := range params {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			pairs := make([]string, 0, len(keys))
			for _, k := range keys {
				pairs = append(pairs, k+"="+params.Get(k))
			}
			builder.WriteString(strings.Join(pairs, "&"))
		}
	}

	if cfg.Parts.Body {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewBuffer(body))
		builder.Write(body)
	}

	return []byte(builder.String()), nil
}
