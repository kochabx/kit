package util

import (
	"encoding/base64"
	"math/rand"
	"net"
	"os"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
)

func Id() string {
	return uuid.New().String()
}

func IpV4() string {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr := conn.LocalAddr().(*net.UDPAddr)
	ip := strings.Split(addr.String(), ":")[0]
	return ip
}

func Hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}

	return hostname
}

// GenerateRandomCode generates a random code with the specified length.
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

// QRCode generates a QR code and returns a Base64 encoded string
func QRCode(content string, size int) (string, error) {
	bytes, err := qrcode.Encode(content, qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// MobileDesensitization desensitizes the mobile number by replacing the middle part with "*".
func MobileDesensitization(mobile string) string {
	length := len(mobile)
	if length < 7 {
		return mobile
	}
	return mobile[:3] + "****" + mobile[length-3:]
}

// EmailDesensitization desensitizes the email by replacing the middle part with "*".
func EmailDesensitization(email string) string {
	index := strings.IndexByte(email, '@')
	if index == -1 || index < 4 {
		return email
	}
	return email[:3] + "****" + email[index:]
}
