package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

const (
	// Default configuration values.
	DefaultSecretSize   = 16  // Default secret length in bytes.
	DefaultExpireSecond = 30  // Default code lifetime in seconds.
	DefaultDigits       = 6   // Default number of code digits.
	DefaultQRCodeSize   = 256 // Default QR code size in pixels.
	DefaultTimeWindow   = 1   // Default number of adjacent time windows accepted for clock skew.

	// TOTP algorithm constants.
	HashOffset     = 0x0F       // Dynamic truncation offset mask.
	DynamicCodeLen = 4          // Dynamic binary code length in bytes.
	MaxUint32      = 0x7FFFFFFF // Mask that clears the sign bit of a 32-bit value.

	// Validation bounds.
	MinSecretSize = 8   // Minimum secret length in bytes.
	MaxSecretSize = 128 // Maximum secret length in bytes.
	MinDigits     = 4   // Minimum number of code digits.
	MaxDigits     = 10  // Maximum number of code digits.
	MaxTimeWindow = 10  // Maximum number of adjacent time windows.
)

// TOTPScheme is the URI prefix for TOTP provisioning.
const TOTPScheme = "otpauth://totp/"

// Error values.
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

// GoogleAuthenticatorer defines the Google Authenticator operations.
type GoogleAuthenticatorer interface {
	GenerateSecret() string
	GenerateCode(secret string) (string, error)
	GenerateQRCode(label string, issuer string, secret string) (string, error)
	ValidateCode(secret, code string) (bool, error)
}

// GA is the shared authenticator instance.
var GA GoogleAuthenticatorer

func init() {
	GA = NewGoogleAuthenticator()
}

// GoogleAuthenticator implements time-based one-time passwords (TOTP).
type GoogleAuthenticator struct {
	SecretSize   int `json:"secret_size"`   // Length of generated secrets in bytes.
	ExpireSecond int `json:"expire_second"` // Time step in seconds.
	Digits       int `json:"digits"`        // Number of digits in generated codes.
	QrCodeSize   int `json:"qr_code_size"`  // QR code image size in pixels.
	TimeWindow   int `json:"time_window"`   // Number of adjacent time steps accepted for clock skew.

	digitsMod uint32           // Precomputed modulus, 10^digits.
	encoder   *base32.Encoding // Reusable Base32 encoder.
}

// Option configures a GoogleAuthenticator.
type Option func(*GoogleAuthenticator)

// WithSecretSize sets the secret length in bytes.
func WithSecretSize(secretSize int) Option {
	return func(ga *GoogleAuthenticator) {
		if secretSize >= MinSecretSize && secretSize <= MaxSecretSize {
			ga.SecretSize = secretSize
		}
	}
}

// WithExpireSecond sets the time step in seconds.
func WithExpireSecond(expireSecond int) Option {
	return func(ga *GoogleAuthenticator) {
		if expireSecond > 0 {
			ga.ExpireSecond = expireSecond
		}
	}
}

// WithDigits sets the number of digits in generated codes.
func WithDigits(digits int) Option {
	return func(ga *GoogleAuthenticator) {
		if digits >= MinDigits && digits <= MaxDigits {
			ga.Digits = digits
			ga.digitsMod = uint32(math.Pow10(digits))
		}
	}
}

// WithQrCodeSize sets the QR code image size in pixels.
func WithQrCodeSize(qrCodeSize int) Option {
	return func(ga *GoogleAuthenticator) {
		if qrCodeSize > 0 {
			ga.QrCodeSize = qrCodeSize
		}
	}
}

// WithTimeWindow sets the number of adjacent time steps accepted for clock skew.
func WithTimeWindow(timeWindow int) Option {
	return func(ga *GoogleAuthenticator) {
		if timeWindow >= 0 && timeWindow <= MaxTimeWindow {
			ga.TimeWindow = timeWindow
		}
	}
}

// NewGoogleAuthenticator creates an authenticator with defaults and the supplied options.
func NewGoogleAuthenticator(opts ...Option) GoogleAuthenticatorer {
	ga := &GoogleAuthenticator{
		SecretSize:   DefaultSecretSize,
		ExpireSecond: DefaultExpireSecond,
		Digits:       DefaultDigits,
		QrCodeSize:   DefaultQRCodeSize,
		TimeWindow:   DefaultTimeWindow,
		digitsMod:    uint32(math.Pow10(DefaultDigits)),                // Precompute the modulus.
		encoder:      base32.StdEncoding.WithPadding(base32.NoPadding), // Reuse the encoder.
	}

	for _, opt := range opts {
		opt(ga)
	}

	return ga
}

// GenerateSecret returns a Base32-encoded secret.
func (ga *GoogleAuthenticator) GenerateSecret() string {
	// Read secret bytes from crypto/rand.
	secret := make([]byte, ga.SecretSize)
	if _, err := rand.Read(secret); err != nil {
		// Fall back to timestamp-based generation if the random read fails.
		return ga.generateTimeBasedSecret()
	}

	// Encode the secret as Base32.
	return strings.ToUpper(ga.encoder.EncodeToString(secret))
}

// generateTimeBasedSecret generates a timestamp-based fallback secret.
func (ga *GoogleAuthenticator) generateTimeBasedSecret() string {
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, uint64(time.Now().UnixNano()))

	// Mix the timestamp with a fixed seed.
	hash := ga.hmacSha1(timeBytes, []byte("fallback-seed"))
	secretLen := min(len(hash), ga.SecretSize)
	return strings.ToUpper(ga.encoder.EncodeToString(hash[:secretLen]))
}

// GenerateCode generates a TOTP code for the secret at the current time.
func (ga *GoogleAuthenticator) GenerateCode(secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	return ga.generateCodeAtTime(secret, time.Now().Unix())
}

// generateCodeAtTime generates a TOTP code for the given Unix timestamp.
func (ga *GoogleAuthenticator) generateCodeAtTime(secret string, timestamp int64) (string, error) {
	// Decode the Base32 secret.
	secretBytes, err := ga.base32decode(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSecretDecode, err)
	}

	// Calculate the time counter.
	timeCounter := uint64(timestamp) / uint64(ga.ExpireSecond)

	// Generate the HMAC-SHA-1 digest.
	hash := ga.generateHMAC(secretBytes, timeCounter)

	// Extract the dynamic binary code.
	code := ga.extractDynamicCode(hash)

	// Format the code with leading zeros.
	return fmt.Sprintf(fmt.Sprintf("%%0%dd", ga.Digits), code), nil
}

// generateHMAC computes HMAC-SHA-1 for the secret and counter.
func (ga *GoogleAuthenticator) generateHMAC(secret []byte, counter uint64) []byte {
	// Encode the counter as eight big-endian bytes.
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)

	// Compute HMAC-SHA-1.
	mac := hmac.New(sha1.New, secret)
	mac.Write(counterBytes)
	return mac.Sum(nil)
}

// extractDynamicCode extracts the truncated code from an HMAC digest.
func (ga *GoogleAuthenticator) extractDynamicCode(hash []byte) uint32 {
	// Use the last four bits of the digest as the offset.
	offset := hash[len(hash)-1] & HashOffset

	// Extract the four-byte dynamic binary code.
	dynamicBinaryCode := binary.BigEndian.Uint32(hash[offset : offset+DynamicCodeLen])

	// Clear the sign bit.
	dynamicBinaryCode &= MaxUint32

	// Apply the precomputed modulus.
	return dynamicBinaryCode % ga.digitsMod
}

// GenerateQRCode generates a QR code for TOTP provisioning.
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
	qrCode, err := generateQRCode(qrData, ga.QrCodeSize)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrQRCodeGeneration, err)
	}
	return qrCode, nil
}

func generateQRCode(content string, size int) (string, error) {
	png, err := qrcode.Encode(content, qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

// generateQRData builds the TOTP provisioning URI.
func (ga *GoogleAuthenticator) generateQRData(label, issuer, secret string) string {
	// URL-encode the label.
	encodedLabel := url.QueryEscape(label)

	// Build the TOTP URI query parameters.
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", strconv.Itoa(ga.Digits))
	params.Set("period", strconv.Itoa(ga.ExpireSecond))

	return fmt.Sprintf("%s%s?%s", TOTPScheme, encodedLabel, params.Encode())
}

// ValidateCode checks a TOTP code within the configured time window.
func (ga *GoogleAuthenticator) ValidateCode(secret, code string) (bool, error) {
	if secret == "" {
		return false, ErrEmptySecret
	}
	if code == "" {
		return false, ErrEmptyCode
	}

	currentTime := time.Now().Unix()

	// Check the current and adjacent time steps to allow clock skew.
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

// base32decode decodes a Base32 string into bytes.
func (ga *GoogleAuthenticator) base32decode(encoded string) ([]byte, error) {
	// Add padding if needed.
	if len(encoded)%8 != 0 {
		encoded += strings.Repeat("=", 8-len(encoded)%8)
	}

	// Decode Base32.
	decoded, err := base32.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBase32Decode, err)
	}
	return decoded, nil
}

// hmacSha1 computes HMAC-SHA-1.
func (ga *GoogleAuthenticator) hmacSha1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
	if len(data) > 0 {
		mac.Write(data)
	}
	return mac.Sum(nil)
}
