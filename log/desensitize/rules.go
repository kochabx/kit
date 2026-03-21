package desensitize

import (
	"fmt"
	"regexp"
)

// Rule 脱敏规则接口
type Rule interface {
	// Name 返回规则名称
	Name() string
}

// ContentRule 基于内容匹配的脱敏规则
type ContentRule struct {
	name        string
	pattern     *regexp.Regexp
	replacement string
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

// FieldRule 基于字段名匹配的脱敏规则
type FieldRule struct {
	name         string
	fieldName    string
	fieldPattern *regexp.Regexp
	replacement  string
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

	return &FieldRule{
		name:         name,
		fieldName:    fieldName,
		fieldPattern: fieldPattern,
		replacement:  replacement,
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
