package qrcode

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerate(t *testing.T) {
	content := "https://example.com"
	size := 256

	result, err := Generate(content, size)
	assert.NoError(t, err)
	assert.NotEmpty(t, result)

	// Verify it's valid base64
	_, err = base64.StdEncoding.DecodeString(result)
	assert.NoError(t, err)
}

func TestGenerateWithLevel(t *testing.T) {
	content := "test content"
	size := 256

	levels := []ErrorCorrectionLevel{Low, Medium, High, Highest}
	for _, level := range levels {
		result, err := GenerateWithLevel(content, size, level)
		assert.NoError(t, err)
		assert.NotEmpty(t, result)
	}
}

func TestGenerateToFile(t *testing.T) {
	content := "test"
	size := 256
	filename := "/tmp/test_qrcode.png"

	err := GenerateToFile(content, size, filename)
	assert.NoError(t, err)

	// Clean up
	defer os.Remove(filename)

	// Verify file exists
	_, err = os.Stat(filename)
	assert.NoError(t, err)
}

func TestGenerateBytes(t *testing.T) {
	content := "test"
	size := 256

	bytes, err := GenerateBytes(content, size)
	assert.NoError(t, err)
	assert.NotEmpty(t, bytes)
}

func BenchmarkGenerate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = Generate("https://example.com", 256)
	}
}
