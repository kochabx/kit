package qrcode

import (
	"encoding/base64"

	"github.com/skip2/go-qrcode"
)

// ErrorCorrectionLevel 二维码纠错级别
type ErrorCorrectionLevel = qrcode.RecoveryLevel

const (
	// Low 7% 的纠错能力
	Low ErrorCorrectionLevel = qrcode.Low
	// Medium 15% 的纠错能力（默认）
	Medium ErrorCorrectionLevel = qrcode.Medium
	// High 25% 的纠错能力
	High ErrorCorrectionLevel = qrcode.High
	// Highest 30% 的纠错能力
	Highest ErrorCorrectionLevel = qrcode.Highest
)

// Generate 生成二维码并返回 Base64 编码的字符串
func Generate(content string, size int) (string, error) {
	return GenerateWithLevel(content, size, Medium)
}

// GenerateWithLevel 生成指定纠错级别的二维码并返回 Base64 编码的字符串
func GenerateWithLevel(content string, size int, level ErrorCorrectionLevel) (string, error) {
	bytes, err := qrcode.Encode(content, level, size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// GenerateToFile 生成二维码并保存到文件
func GenerateToFile(content string, size int, filename string) error {
	return qrcode.WriteFile(content, Medium, size, filename)
}

// GenerateBytes 生成二维码并返回字节数组
func GenerateBytes(content string, size int) ([]byte, error) {
	return qrcode.Encode(content, Medium, size)
}
