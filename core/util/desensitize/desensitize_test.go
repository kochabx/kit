package desensitize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMobile(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{"normal", "13812345678", "138****5678"},
		{"short", "123456", "123456"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Mobile(tt.input)
			assert.Equal(t, tt.output, result)
		})
	}
}

func TestEmail(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{"normal", "test@example.com", "tes****@example.com"},
		{"short", "te@a.com", "te@a.com"},
		{"no at", "test", "test"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Email(tt.input)
			assert.Equal(t, tt.output, result)
		})
	}
}

func TestIDCard(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{"18 digits", "110101199001011234", "1101**********1234"},
		{"short", "1234567", "1234567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IDCard(tt.input)
			assert.Equal(t, tt.output, result)
		})
	}
}

func TestBankCard(t *testing.T) {
	input := "6222021234567890123"
	expected := "6222***********0123"
	result := BankCard(input)
	assert.Equal(t, expected, result)
}

func TestName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{"2 chars", "张三", "张*"},
		{"3 chars", "张三丰", "张**"},
		{"1 char", "张", "张"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Name(tt.input)
			assert.Equal(t, tt.output, result)
		})
	}
}

func TestAddress(t *testing.T) {
	input := "北京市朝阳区某某街道123号"
	result := Address(input)
	assert.True(t, len([]rune(result)) > 6)
	assert.Contains(t, result, "北京市朝阳区")
}

func TestCustom(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		keep   int
		output string
	}{
		{"keep 2", "1234567890", 2, "12******90"},
		{"keep 3", "1234567890", 3, "123****890"},
		{"short", "12345", 3, "12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Custom(tt.input, tt.keep)
			assert.Equal(t, tt.output, result)
		})
	}
}
