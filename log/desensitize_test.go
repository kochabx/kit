package log

import (
	"strings"
	"testing"

	"github.com/kochabx/kit/log/desensitize"
)

func TestDesensitizeHook(t *testing.T) {
	// 创建脱敏钩子
	hook := desensitize.NewHook()

	// 添加手机号脱敏规则
	err := hook.AddContentRule("phone", `1[3-9]\d{9}`, "1****5678")
	if err != nil {
		t.Fatalf("Failed to add phone rule: %v", err)
	}

	// 添加邮箱脱敏规则
	err = hook.AddContentRule("email", `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, "***@***.com")
	if err != nil {
		t.Fatalf("Failed to add email rule: %v", err)
	}

	// 测试脱敏功能
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "phone number",
			input:    "用户手机号：13812345678",
			expected: "用户手机号：1****5678",
		},
		{
			name:     "email address",
			input:    "用户邮箱：test@example.com",
			expected: "用户邮箱：***@***.com",
		},
		{
			name:     "mixed content",
			input:    "联系方式：13912345678，邮箱：user@test.org",
			expected: "联系方式：1****5678，邮箱：***@***.com",
		},
		{
			name:     "no sensitive data",
			input:    "这是一条普通的日志消息",
			expected: "这是一条普通的日志消息",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := hook.Desensitize(tc.input)
			if result != tc.expected {
				t.Errorf("Expected: %s, Got: %s", tc.expected, result)
			}
		})
	}
}

func TestDesensitizeHookRuleManagement(t *testing.T) {
	hook := desensitize.NewHook()

	// 测试添加规则
	err := hook.AddContentRule("test", `test\d+`, "TEST****")
	if err != nil {
		t.Fatalf("Failed to add rule: %v", err)
	}

	// 测试获取规则
	ruleNames := hook.GetRules()
	if len(ruleNames) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(ruleNames))
	}

	rule, exists := hook.GetRule("test")
	if !exists {
		t.Error("Rule 'test' should exist")
	}
	if !rule.Enabled() {
		t.Error("Rule should be enabled by default")
	}

	// 测试禁用规则
	hook.DisableRule("test")
	text := "test123"
	result := hook.Desensitize(text)
	if result != text {
		t.Errorf("Disabled rule should not affect text. Expected: %s, Got: %s", text, result)
	}

	// 测试启用规则
	hook.EnableRule("test")
	result = hook.Desensitize(text)
	if result != "TEST****" {
		t.Errorf("Enabled rule should affect text. Expected: TEST****, Got: %s", result)
	}

	// 测试移除规则
	hook.RemoveRule("test")
	ruleNames = hook.GetRules()
	if len(ruleNames) != 0 {
		t.Errorf("Expected 0 rules after removal, got %d", len(ruleNames))
	}
}

func TestDesensitizeHookInvalidPattern(t *testing.T) {
	hook := desensitize.NewHook()

	// 测试无效的正则表达式
	err := hook.AddContentRule("invalid", "[", "replacement")
	if err == nil {
		t.Error("Expected error for invalid regex pattern")
	}
}

func TestDesensitizeWriter(t *testing.T) {
	hook := desensitize.NewHook()
	hook.AddContentRule("phone", `1[3-9]\d{9}`, "1****5678")

	// 创建一个 string builder 作为目标 writer
	var buf strings.Builder
	writer := desensitize.NewWriter(&buf, hook)

	// 写入包含敏感信息的内容
	content := "用户登录，手机号：13812345678"
	n, err := writer.Write([]byte(content))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	if n != len("用户登录，手机号：1****5678") {
		t.Errorf("Expected to write %d bytes, wrote %d", len("用户登录，手机号：1****5678"), n)
	}

	result := buf.String()
	expected := "用户登录，手机号：1****5678"
	if result != expected {
		t.Errorf("Expected: %s, Got: %s", expected, result)
	}
}

func TestLoggerWithDesensitize(t *testing.T) {
	hook := desensitize.NewHook()
	hook.AddFieldRule("phone_pattern", "phone", `^(\d{3})(\d+)(\d{4})$`, "$1****$3")

	// 创建带脱敏功能的logger
	logger := New(WithDesensitize(hook))

	// 验证脱敏钩子已设置
	if logger.GetDesensitizeHook() == nil {
		t.Error("Desensitize hook should be set")
	}

	// 测试手机号会被脱敏
	logger.Info().Str("phone", "13812345678").Msg("Test desensitize phone number")

	// 测试邮箱不会被脱敏（因为没有添加邮箱脱敏规则）
	logger.Info().Str("email", "user@example.com").Msg("Test email should not be desensitized")

	// 验证脱敏钩子是同一个实例
	if logger.GetDesensitizeHook() != hook {
		t.Error("Desensitize hook should be the same instance")
	}
}
