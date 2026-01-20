package middleware

import (
	"bytes"
	"encoding/base64"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/core/crypto/ecies"
	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/transport/http"
)

var (
	ErrDecryptFailed = errors.Internal("decrypt request body failed")
)

// Decryptor 解密器接口
type Decryptor interface {
	Decrypt(ciphertext []byte) ([]byte, error)
}

// DecryptorFunc 解密器函数适配器
type DecryptorFunc func(ciphertext []byte) ([]byte, error)

func (f DecryptorFunc) Decrypt(ciphertext []byte) ([]byte, error) {
	return f(ciphertext)
}

// CryptoConfig 加解密中间件配置
type CryptoConfig struct {
	Decryptor    Decryptor                 // 解密器（必需）
	Base64Decode bool                      // 是否对请求体进行 Base64 解码，默认 true
	SkipPaths    []string                  // 跳过处理的路径前缀
	SkipFunc     func(*gin.Context) bool   // 动态跳过判断函数
	ErrorHandler func(*gin.Context, error) // 错误处理函数
	Logger       *log.Logger               // 自定义日志记录器
}

// ECIESDecryptor 创建基于 ECIES 的解密器
func ECIESDecryptor(privateKeyPath string) (Decryptor, error) {
	privateKey, err := ecies.LoadPrivateKey(privateKeyPath)
	if err != nil {
		return nil, err
	}
	return DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
		return ecies.Decrypt(privateKey, ciphertext)
	}), nil
}

// MustECIESDecryptor 创建基于 ECIES 的解密器，失败时 panic
func MustECIESDecryptor(privateKeyPath string) Decryptor {
	d, err := ECIESDecryptor(privateKeyPath)
	if err != nil {
		panic(err)
	}
	return d
}

// Crypto 创建加解密中间件
func Crypto(cfg CryptoConfig) gin.HandlerFunc {
	if cfg.Decryptor == nil {
		panic("middleware: Decryptor is required")
	}

	// 默认启用 Base64 解码
	if !cfg.Base64Decode {
		cfg.Base64Decode = true
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(c *gin.Context, err error) {
			http.GinJSONE(c, http.StatusBadRequest, ErrDecryptFailed)
			c.Abort()
		}
	}

	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	// 预编译路径匹配器
	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(c *gin.Context) {
		// 检查是否跳过
		if shouldSkip(c, matcher, cfg.SkipFunc) {
			c.Next()
			return
		}

		// 读取请求体
		body, err := c.GetRawData()
		if err != nil {
			cfg.Logger.Error().Err(err).Msg("crypto: read request body failed")
			cfg.ErrorHandler(c, err)
			return
		}

		// 空请求体直接跳过
		if len(body) == 0 {
			c.Next()
			return
		}

		// Base64 解码
		ciphertext := body
		if cfg.Base64Decode {
			decoded, err := base64.StdEncoding.DecodeString(string(body))
			if err != nil {
				cfg.Logger.Error().Err(err).Msg("crypto: base64 decode failed")
				cfg.ErrorHandler(c, err)
				return
			}
			ciphertext = decoded
		}

		// 解密
		plaintext, err := cfg.Decryptor.Decrypt(ciphertext)
		if err != nil {
			cfg.Logger.Error().Err(err).Msg("crypto: decrypt failed")
			cfg.ErrorHandler(c, err)
			return
		}

		// 将解密后的数据写回请求体
		c.Request.Body = io.NopCloser(bytes.NewBuffer(plaintext))
		c.Request.ContentLength = int64(len(plaintext))

		c.Next()
	}
}
