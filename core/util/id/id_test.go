package id

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerate(t *testing.T) {
	id1 := Generate()
	id2 := Generate()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 36) // UUID standard length
}

func TestUUID(t *testing.T) {
	id := UUID()
	assert.NotEmpty(t, id)
	assert.Len(t, id, 36)
}

func TestGenerateRandomCode(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"4 digits", 4},
		{"6 digits", 6},
		{"8 digits", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := GenerateRandomCode(tt.length)
			assert.Len(t, code, tt.length)
			// Verify all characters are digits
			for _, c := range code {
				assert.True(t, c >= '0' && c <= '9')
			}
		})
	}
}

func BenchmarkGenerate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Generate()
	}
}

func BenchmarkGenerateRandomCode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GenerateRandomCode(6)
	}
}
