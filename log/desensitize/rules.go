package desensitize

import (
	"fmt"
	"regexp"
	"sync/atomic"
)

// Rule 脱敏规则接口
type Rule interface {
	// Name 返回规则名称
	Name() string
	// Enabled 返回规则是否启用
	Enabled() bool
	// SetEnabled 设置规则启用状态
	SetEnabled(enabled bool)
	// Process 对字符串进行脱敏处理
	Process(s string) string
}

// ContentRule 基于内容匹配的脱敏规则
type ContentRule struct {
	name        string
	pattern     *regexp.Regexp
	replacement string
	enabled     int32 // 使用原子操作避免锁竞争
}

// NewContentRule 创建基于内容匹配的脱敏规则
func NewContentRule(name, pattern, replacement string) (*ContentRule, error) {
	if name == "" {
		return nil, fmt.Errorf("rule name cannot be empty")
	}
	if pattern == "" {
		return nil, fmt.Errorf("pattern cannot be empty")
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern '%s': %w", pattern, err)
	}

	return &ContentRule{
		name:        name,
		pattern:     regex,
		replacement: replacement,
		enabled:     1,
	}, nil
}

// MustNewContentRule 创建规则，如果失败则 panic（用于内置规则）
func MustNewContentRule(name, pattern, replacement string) *ContentRule {
	rule, err := NewContentRule(name, pattern, replacement)
	if err != nil {
		panic(err)
	}
	return rule
}

func (r *ContentRule) Name() string {
	return r.name
}

func (r *ContentRule) Enabled() bool {
	return atomic.LoadInt32(&r.enabled) == 1
}

func (r *ContentRule) SetEnabled(enabled bool) {
	if enabled {
		atomic.StoreInt32(&r.enabled, 1)
	} else {
		atomic.StoreInt32(&r.enabled, 0)
	}
}

func (r *ContentRule) Process(s string) string {
	if !r.Enabled() {
		return s
	}
	return r.pattern.ReplaceAllString(s, r.replacement)
}

// FieldRule 基于字段名匹配的脱敏规则
type FieldRule struct {
	name         string
	fieldName    string
	fieldPattern *regexp.Regexp
	replacement  string
	jsonPattern  *regexp.Regexp // 预编译的JSON字段匹配模式
	enabled      int32
}

// NewFieldRule 创建基于字段名匹配的脱敏规则
func NewFieldRule(name, fieldName, pattern, replacement string) (*FieldRule, error) {
	if name == "" {
		return nil, fmt.Errorf("rule name cannot be empty")
	}
	if fieldName == "" {
		return nil, fmt.Errorf("field name cannot be empty")
	}
	if pattern == "" {
		return nil, fmt.Errorf("pattern cannot be empty")
	}

	fieldPattern, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid field pattern '%s': %w", pattern, err)
	}

	// 预编译JSON字段匹配模式
	jsonPatternStr := fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, regexp.QuoteMeta(fieldName))
	jsonPattern, err := regexp.Compile(jsonPatternStr)
	if err != nil {
		return nil, fmt.Errorf("failed to compile json pattern: %w", err)
	}

	return &FieldRule{
		name:         name,
		fieldName:    fieldName,
		fieldPattern: fieldPattern,
		replacement:  replacement,
		jsonPattern:  jsonPattern,
		enabled:      1,
	}, nil
}

// MustNewFieldRule 创建规则，如果失败则 panic
func MustNewFieldRule(name, fieldName, pattern, replacement string) *FieldRule {
	rule, err := NewFieldRule(name, fieldName, pattern, replacement)
	if err != nil {
		panic(err)
	}
	return rule
}

func (r *FieldRule) Name() string {
	return r.name
}

func (r *FieldRule) Enabled() bool {
	return atomic.LoadInt32(&r.enabled) == 1
}

func (r *FieldRule) SetEnabled(enabled bool) {
	if enabled {
		atomic.StoreInt32(&r.enabled, 1)
	} else {
		atomic.StoreInt32(&r.enabled, 0)
	}
}

func (r *FieldRule) Process(s string) string {
	if !r.Enabled() {
		return s
	}

	return r.jsonPattern.ReplaceAllStringFunc(s, func(match string) string {
		submatches := r.jsonPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		fieldValue := submatches[1]
		newValue := r.fieldPattern.ReplaceAllString(fieldValue, r.replacement)

		// 保持字段名和引号，只替换值
		return fmt.Sprintf(`"%s":"%s"`, r.fieldName, newValue)
	})
}
