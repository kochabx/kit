package desensitize

import (
	"bytes"
	"testing"
)

// 模拟典型 JSON 日志行
var benchInput = []byte(`{"level":"info","time":"2026-03-21 12:00:00","caller":"handler.go:42","message":"user login","phone":"13812345678","password":"secret123","token":"eyJhbGciOiJIUzI1NiJ9.payload.sig","email":"user@example.com","idcard":"110101199003079234"}`)

func newBenchHook() *Hook {
	hook := NewHook()
	hook.AddBuiltin(BuiltinRules()...)
	hook.AddBuiltin(PhoneRule, EmailRule)
	return hook
}

// BenchmarkDesensitize 测试 Desensitize 热路径
func BenchmarkDesensitize(b *testing.B) {
	hook := newBenchHook()
	input := string(benchInput)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hook.Desensitize(input)
	}
}

// BenchmarkDesensitize_NoMatch 命中率低的场景（内容不含敏感信息）
func BenchmarkDesensitize_NoMatch(b *testing.B) {
	hook := newBenchHook()
	input := `{"level":"info","time":"2026-03-21 12:00:00","message":"system started"}`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hook.Desensitize(input)
	}
}

// BenchmarkWriter_Write 测试 Writer.Write 完整路径
func BenchmarkWriter_Write(b *testing.B) {
	hook := newBenchHook()
	buf := &bytes.Buffer{}
	w := NewWriter(buf, hook)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = w.Write(benchInput)
	}
}

// BenchmarkWriter_Write_NoRules 无规则快速路径
func BenchmarkWriter_Write_NoRules(b *testing.B) {
	hook := NewHook()
	buf := &bytes.Buffer{}
	w := NewWriter(buf, hook)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = w.Write(benchInput)
	}
}
