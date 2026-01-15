package id

import (
	"math/rand"
	"time"

	"github.com/google/uuid"
)

// Generate 生成 UUID
func Generate() string {
	return uuid.New().String()
}

// UUID 生成 UUID (别名)
func UUID() string {
	return Generate()
}

// GenerateRandomCode 生成指定长度的随机数字代码
func GenerateRandomCode(length int) string {
	source := rand.NewSource(time.Now().UnixNano())
	r := rand.New(source)
	digits := "0123456789"
	code := make([]byte, length)
	for i := range code {
		code[i] = digits[r.Intn(len(digits))]
	}
	return string(code)
}
