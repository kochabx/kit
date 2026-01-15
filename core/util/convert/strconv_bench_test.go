package convert

import (
	"testing"
)

func BenchmarkParseStrings_Int(b *testing.B) {
	input := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseStrings[int](input)
	}
}

func BenchmarkParseStrings_Int64(b *testing.B) {
	input := []string{"100", "200", "300", "400", "500"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseStrings[int64](input)
	}
}

func BenchmarkParseStrings_Float64(b *testing.B) {
	input := []string{"1.1", "2.2", "3.3", "4.4", "5.5"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseStrings[float64](input)
	}
}

func BenchmarkParseString_Int(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseString[int]("42")
	}
}

func BenchmarkParseString_Float64(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseString[float64]("3.14159")
	}
}
