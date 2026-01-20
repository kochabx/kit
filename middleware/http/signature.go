package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/transport/http"
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

// SignatureConfig 签名验证中间件配置
type SignatureConfig struct {
	Signer        Signer                    // 签名验证器（必需）
	HeaderName    string                    // 签名头名称，默认 "X-Signature"
	ParamsEnabled bool                      // 是否包含 Query 参数
	BodyEnabled   bool                      // 是否包含请求体
	PathEnabled   bool                      // 是否包含请求路径
	MethodEnabled bool                      // 是否包含请求方法
	SkipPaths     []string                  // 跳过处理的路径前缀
	SkipFunc      func(*gin.Context) bool   // 动态跳过判断函数
	ErrorHandler  func(*gin.Context, error) // 错误处理函数
	Logger        *log.Logger               // 自定义日志记录器
}

// DefaultSignatureConfig 返回默认签名配置
func DefaultSignatureConfig() SignatureConfig {
	return SignatureConfig{
		HeaderName:    "X-Signature",
		ParamsEnabled: true,
		BodyEnabled:   true,
		PathEnabled:   false,
		MethodEnabled: false,
	}
}

// Signature 创建签名验证中间件
func Signature(cfgs ...SignatureConfig) gin.HandlerFunc {
	cfg := SignatureConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	// 设置默认日志记录器
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
		cfg.ErrorHandler = func(c *gin.Context, err error) {
			http.GinJSONE(c, http.StatusBadRequest, ErrSignatureFailed)
			c.Abort()
		}
	}

	// 预编译路径匹配器
	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(c *gin.Context) {
		// 检查是否跳过
		if shouldSkip(c, matcher, cfg.SkipFunc) {
			c.Next()
			return
		}

		// 获取签名
		signature := c.GetHeader(cfg.HeaderName)
		if signature == "" {
			cfg.Logger.Error().Str("header", cfg.HeaderName).Msg("signature: header missing")
			cfg.ErrorHandler(c, ErrSignatureFailed)
			return
		}

		// 构建待签名数据
		data, err := buildSignatureData(c, cfg)
		if err != nil {
			cfg.Logger.Error().Err(err).Msg("signature: build data failed")
			cfg.ErrorHandler(c, err)
			return
		}

		// 验证签名
		if err := cfg.Signer.Verify(data, signature); err != nil {
			cfg.Logger.Error().Err(err).Msg("signature: verify failed")
			cfg.ErrorHandler(c, err)
			return
		}

		c.Next()
	}
}

// buildSignatureData 构建待签名数据
func buildSignatureData(c *gin.Context, cfg SignatureConfig) ([]byte, error) {
	var builder strings.Builder

	// 添加请求方法
	if cfg.MethodEnabled {
		builder.WriteString(c.Request.Method)
	}

	// 添加请求路径
	if cfg.PathEnabled {
		builder.WriteString(c.Request.URL.Path)
	}

	// 添加 Query 参数（按 key 排序）
	if cfg.ParamsEnabled {
		params := c.Request.URL.Query()
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

	// 添加请求体
	if cfg.BodyEnabled {
		body, err := c.GetRawData()
		if err != nil {
			return nil, err
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		builder.Write(body)
	}

	return []byte(builder.String()), nil
}
