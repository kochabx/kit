package hmac

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/kochabx/kit/errors"
)

// SignResult 封装签名结果
type SignResult struct {
	Signature string
	Timestamp int64
}

// Option 用于配置签名和验证选项
type Option struct {
	payload    string
	expiration time.Duration
	now        func() time.Time // 用于测试的时间注入
}

// WithPayload 设置要签名的附加数据
func WithPayload(payload string) func(*Option) {
	return func(o *Option) {
		o.payload = payload
	}
}

// WithExpiration 设置签名过期时间，默认为 5 分钟
func WithExpiration(d time.Duration) func(*Option) {
	return func(o *Option) {
		o.expiration = d
	}
}

// withTimeFunc 用于测试的时间注入（内部使用）
func withTimeFunc(fn func() time.Time) func(*Option) {
	return func(o *Option) {
		o.now = fn
	}
}

// Sign 生成 HMAC-SHA256 签名
// secret: 密钥
// opts: 可选配置，支持 WithPayload 和 WithExpiration
// 返回签名结果，包含签名和时间戳
func Sign(secret string, opts ...func(*Option)) (*SignResult, error) {
	if secret == "" {
		return nil, errors.BadRequest("secret cannot be empty")
	}

	opt := &Option{
		expiration: 5 * time.Minute,
		now:        time.Now,
	}
	for _, o := range opts {
		o(opt)
	}

	timestamp := opt.now().Unix()
	signature := generateSignature(secret, timestamp, opt.payload)

	return &SignResult{
		Signature: signature,
		Timestamp: timestamp,
	}, nil
}

// Verify 验证 HMAC-SHA256 签名
// secret: 密钥
// signature: 待验证的签名
// timestamp: 签名时的时间戳
// opts: 可选配置，需要与签名时保持一致
func Verify(secret, signature string, timestamp int64, opts ...func(*Option)) error {
	if secret == "" {
		return errors.BadRequest("secret cannot be empty")
	}
	if signature == "" {
		return errors.BadRequest("signature cannot be empty")
	}
	if timestamp <= 0 {
		return errors.BadRequest("invalid timestamp")
	}

	opt := &Option{
		expiration: 5 * time.Minute,
		now:        time.Now,
	}
	for _, o := range opts {
		o(opt)
	}

	// 检查时间戳是否过期
	elapsed := opt.now().Unix() - timestamp
	if elapsed > int64(opt.expiration.Seconds()) {
		return errors.BadRequest("signature expired")
	}
	if elapsed < 0 {
		return errors.BadRequest("timestamp is in the future")
	}

	// 生成期望的签名
	expectedSignature := generateSignature(secret, timestamp, opt.payload)

	// 使用常量时间比较防止时序攻击
	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		return errors.BadRequest("invalid signature format")
	}
	expectedBytes, _ := hex.DecodeString(expectedSignature)

	if !hmac.Equal(signatureBytes, expectedBytes) {
		return errors.BadRequest("signature mismatch")
	}

	return nil
}

// generateSignature 生成 HMAC-SHA256 签名的内部函数
func generateSignature(secret string, timestamp int64, payload string) string {
	// 预分配容量以提高性能
	var toSign strings.Builder
	toSign.Grow(len(strconv.FormatInt(timestamp, 10)) + 1 + len(secret) + len(payload))

	toSign.WriteString(strconv.FormatInt(timestamp, 10))
	toSign.WriteString("\n")
	toSign.WriteString(secret)
	if payload != "" {
		toSign.WriteString(payload)
	}

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(toSign.String()))
	return hex.EncodeToString(h.Sum(nil))
}
