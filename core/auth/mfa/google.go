package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kochabx/kit/core/util"
)

const (
	// 默认配置值
	DefaultSecretSize   = 16  // 默认密钥长度
	DefaultExpireSecond = 30  // 默认过期时间（秒）
	DefaultDigits       = 6   // 默认验证码位数
	DefaultQRCodeSize   = 256 // 默认二维码尺寸
	DefaultTimeWindow   = 1   // 默认时间窗口容错值，用于处理时钟偏移

	// TOTP算法常量
	HashOffset     = 0x0F       // 哈希偏移掩码
	DynamicCodeLen = 4          // 动态码长度
	MaxUint32      = 0x7FFFFFFF // 最大32位无符号整数

	// 验证相关常量
	MinSecretSize = 8   // 最小密钥长度
	MaxSecretSize = 128 // 最大密钥长度
	MinDigits     = 4   // 最小验证码位数
	MaxDigits     = 10  // 最大验证码位数
	MaxTimeWindow = 10  // 最大时间窗口
)

// TOTP URI 协议
const TOTPScheme = "otpauth://totp/"

// 错误常量定义
var (
	ErrEmptySecret       = errors.New("secret cannot be empty")
	ErrEmptyCode         = errors.New("code cannot be empty")
	ErrEmptyLabel        = errors.New("label cannot be empty")
	ErrEmptyIssuer       = errors.New("issuer cannot be empty")
	ErrInvalidSecretSize = errors.New("secret size must be between 8-128 bytes")
	ErrInvalidDigits     = errors.New("digits must be between 4-10")
	ErrInvalidTimeWindow = errors.New("time window must be between 0-10")
	ErrSecretDecode      = errors.New("failed to decode secret")
	ErrCodeGeneration    = errors.New("failed to generate code")
	ErrQRCodeGeneration  = errors.New("failed to generate QR code")
	ErrRandomGeneration  = errors.New("failed to generate random")
	ErrBase32Decode      = errors.New("failed to decode base32")
)

// GoogleAuthenticatorer 定义了Google身份验证器操作的接口
type GoogleAuthenticatorer interface {
	GenerateSecret() string
	GenerateCode(secret string) (string, error)
	GenerateQRCode(label string, issuer string, secret string) (string, error)
	ValidateCode(secret, code string) (bool, error)
}

// 全局实例
var GA GoogleAuthenticatorer

func init() {
	GA = NewGoogleAuthenticator()
}

// GoogleAuthenticator 实现基于时间的一次性密码(TOTP)算法
type GoogleAuthenticator struct {
	SecretSize   int `json:"secret_size"`   // 生成密钥的长度
	ExpireSecond int `json:"expire_second"` // 时间窗口（秒）
	Digits       int `json:"digits"`        // 生成验证码的位数
	QrCodeSize   int `json:"qr_code_size"`  // 二维码图像尺寸（像素）
	TimeWindow   int `json:"time_window"`   // 时间同步问题的容错值

	digitsMod uint32           // 10^digits，用于模运算
	encoder   *base32.Encoding // 复用的base32编码器
}

// 配置GoogleAuthenticator的选项函数
type Option func(*GoogleAuthenticator)

// WithSecretSize 设置密钥大小
func WithSecretSize(secretSize int) Option {
	return func(ga *GoogleAuthenticator) {
		if secretSize >= MinSecretSize && secretSize <= MaxSecretSize {
			ga.SecretSize = secretSize
		}
	}
}

// WithExpireSecond 设置时间窗口持续时间
func WithExpireSecond(expireSecond int) Option {
	return func(ga *GoogleAuthenticator) {
		if expireSecond > 0 {
			ga.ExpireSecond = expireSecond
		}
	}
}

// WithDigits 设置生成验证码的位数
func WithDigits(digits int) Option {
	return func(ga *GoogleAuthenticator) {
		if digits >= MinDigits && digits <= MaxDigits {
			ga.Digits = digits
			ga.digitsMod = uint32(math.Pow10(digits))
		}
	}
}

// WithQrCodeSize 设置二维码图像尺寸
func WithQrCodeSize(qrCodeSize int) Option {
	return func(ga *GoogleAuthenticator) {
		if qrCodeSize > 0 {
			ga.QrCodeSize = qrCodeSize
		}
	}
}

// WithTimeWindow 设置时间同步的容错值
func WithTimeWindow(timeWindow int) Option {
	return func(ga *GoogleAuthenticator) {
		if timeWindow >= 0 && timeWindow <= MaxTimeWindow {
			ga.TimeWindow = timeWindow
		}
	}
}

// NewGoogleAuthenticator 创建一个具有默认设置的新GoogleAuthenticator
func NewGoogleAuthenticator(opts ...Option) GoogleAuthenticatorer {
	ga := &GoogleAuthenticator{
		SecretSize:   DefaultSecretSize,
		ExpireSecond: DefaultExpireSecond,
		Digits:       DefaultDigits,
		QrCodeSize:   DefaultQRCodeSize,
		TimeWindow:   DefaultTimeWindow,
		digitsMod:    uint32(math.Pow10(DefaultDigits)),                // 预计算模数
		encoder:      base32.StdEncoding.WithPadding(base32.NoPadding), // 复用编码器
	}

	for _, opt := range opts {
		opt(ga)
	}

	return ga
}

// GenerateSecret 生成加密安全的随机密钥
func (ga *GoogleAuthenticator) GenerateSecret() string {
	// 使用 crypto/rand 获得更好的随机性
	secret := make([]byte, ga.SecretSize)
	if _, err := rand.Read(secret); err != nil {
		// 如果 crypto/rand 失败，回退到基于时间的生成
		return ga.generateTimeBasedSecret()
	}

	// 使用base32编码器编码密钥
	return strings.ToUpper(ga.encoder.EncodeToString(secret))
}

// generateTimeBasedSecret 生成基于时间的密钥作为回退方案
func (ga *GoogleAuthenticator) generateTimeBasedSecret() string {
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, uint64(time.Now().UnixNano()))

	// 添加一些额外的熵
	hash := ga.hmacSha1(timeBytes, []byte("fallback-seed"))
	secretLen := min(len(hash), ga.SecretSize)
	return strings.ToUpper(ga.encoder.EncodeToString(hash[:secretLen]))
}

// GenerateCode 为给定密钥在当前时间生成TOTP验证码
func (ga *GoogleAuthenticator) GenerateCode(secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	return ga.generateCodeAtTime(secret, time.Now().Unix())
}

// generateCodeAtTime 为给定密钥在特定时间戳生成TOTP验证码
func (ga *GoogleAuthenticator) generateCodeAtTime(secret string, timestamp int64) (string, error) {
	// 解码base32密钥
	secretBytes, err := ga.base32decode(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSecretDecode, err)
	}

	// 计算时间计数器
	timeCounter := uint64(timestamp) / uint64(ga.ExpireSecond)

	// 生成HMAC-SHA-1哈希
	hash := ga.generateHMAC(secretBytes, timeCounter)

	// 提取动态二进制码
	code := ga.extractDynamicCode(hash)

	// 格式化为带前导零的字符串
	return fmt.Sprintf(fmt.Sprintf("%%0%dd", ga.Digits), code), nil
}

// generateHMAC 为给定密钥和计数器生成HMAC-SHA-1哈希
func (ga *GoogleAuthenticator) generateHMAC(secret []byte, counter uint64) []byte {
	// 将计数器转换为8字节数组
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)

	// 生成HMAC-SHA-1
	mac := hmac.New(sha1.New, secret)
	mac.Write(counterBytes)
	return mac.Sum(nil)
}

// extractDynamicCode 从HMAC哈希中提取动态码
func (ga *GoogleAuthenticator) extractDynamicCode(hash []byte) uint32 {
	// 从哈希的最后4位获取偏移量
	offset := hash[len(hash)-1] & HashOffset

	// 提取4字节动态二进制码
	dynamicBinaryCode := binary.BigEndian.Uint32(hash[offset : offset+DynamicCodeLen])

	// 清除最高位以确保为正数
	dynamicBinaryCode &= MaxUint32

	// 使用预计算的模数进行模运算
	return dynamicBinaryCode % ga.digitsMod
}

// GenerateQRCode 为TOTP设置生成二维码
func (ga *GoogleAuthenticator) GenerateQRCode(label, issuer, secret string) (string, error) {
	if label == "" {
		return "", ErrEmptyLabel
	}
	if issuer == "" {
		return "", ErrEmptyIssuer
	}
	if secret == "" {
		return "", ErrEmptySecret
	}

	qrData := ga.generateQRData(label, issuer, secret)
	qrCode, err := util.QRCode(qrData, ga.QrCodeSize)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrQRCodeGeneration, err)
	}
	return qrCode, nil
}

// generateQRData 为二维码生成TOTP URI
func (ga *GoogleAuthenticator) generateQRData(label, issuer, secret string) string {
	// 为URI编码标签
	encodedLabel := url.QueryEscape(label)

	// 构建具有适当参数的TOTP URI
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", strconv.Itoa(ga.Digits))
	params.Set("period", strconv.Itoa(ga.ExpireSecond))

	return fmt.Sprintf("%s%s?%s", TOTPScheme, encodedLabel, params.Encode())
}

// ValidateCode 验证具有时间窗口容错的TOTP验证码
func (ga *GoogleAuthenticator) ValidateCode(secret, code string) (bool, error) {
	if secret == "" {
		return false, ErrEmptySecret
	}
	if code == "" {
		return false, ErrEmptyCode
	}

	currentTime := time.Now().Unix()

	// 检查当前时间窗口和相邻窗口以处理时钟偏移容错
	for i := -ga.TimeWindow; i <= ga.TimeWindow; i++ {
		windowTime := currentTime + int64(i*ga.ExpireSecond)
		generatedCode, err := ga.generateCodeAtTime(secret, windowTime)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrCodeGeneration, err)
		}

		if generatedCode == code {
			return true, nil
		}
	}

	return false, nil
}

// base32decode 将base32字符串解码为字节数组
func (ga *GoogleAuthenticator) base32decode(encoded string) ([]byte, error) {
	// 如果需要，添加填充
	if len(encoded)%8 != 0 {
		encoded += strings.Repeat("=", 8-len(encoded)%8)
	}

	// 使用base32解码
	decoded, err := base32.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBase32Decode, err)
	}
	return decoded, nil
}

// hmacSha1 生成HMAC-SHA-1哈希
func (ga *GoogleAuthenticator) hmacSha1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
	if len(data) > 0 {
		mac.Write(data)
	}
	return mac.Sum(nil)
}
