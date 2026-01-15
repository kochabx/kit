package desensitize

import (
	"sync"
	"sync/atomic"
)

// Hook 脱敏钩子
type Hook struct {
	rules     sync.Map // name -> Rule
	ruleCount int64    // 规则数量（原子操作）
}

// NewHook 创建新的脱敏钩子
func NewHook() *Hook {
	return &Hook{}
}

// AddRule 添加脱敏规则
func (h *Hook) AddRule(rule Rule) {
	if rule == nil {
		return
	}
	h.rules.Store(rule.Name(), rule)
	atomic.AddInt64(&h.ruleCount, 1)
}

// AddContentRule 添加基于内容匹配的脱敏规则
func (h *Hook) AddContentRule(name, pattern, replacement string) error {
	rule, err := NewContentRule(name, pattern, replacement)
	if err != nil {
		return err
	}
	h.AddRule(rule)
	return nil
}

// AddFieldRule 添加基于字段名匹配的脱敏规则
func (h *Hook) AddFieldRule(name, fieldName, pattern, replacement string) error {
	rule, err := NewFieldRule(name, fieldName, pattern, replacement)
	if err != nil {
		return err
	}
	h.AddRule(rule)
	return nil
}

// AddBuiltin 添加内置规则
func (h *Hook) AddBuiltin(rules ...Rule) {
	for _, rule := range rules {
		h.AddRule(rule)
	}
}

// RemoveRule 移除脱敏规则
func (h *Hook) RemoveRule(name string) bool {
	_, loaded := h.rules.LoadAndDelete(name)
	if loaded {
		atomic.AddInt64(&h.ruleCount, -1)
	}
	return loaded
}

// EnableRule 启用规则
func (h *Hook) EnableRule(name string) bool {
	if rule, ok := h.rules.Load(name); ok {
		if r, ok := rule.(Rule); ok {
			r.SetEnabled(true)
			return true
		}
	}
	return false
}

// DisableRule 禁用规则
func (h *Hook) DisableRule(name string) bool {
	if rule, ok := h.rules.Load(name); ok {
		if r, ok := rule.(Rule); ok {
			r.SetEnabled(false)
			return true
		}
	}
	return false
}

// GetRule 获取指定规则
func (h *Hook) GetRule(name string) (Rule, bool) {
	if rule, ok := h.rules.Load(name); ok {
		if r, ok := rule.(Rule); ok {
			return r, true
		}
	}
	return nil, false
}

// GetRules 列出所有规则名称
func (h *Hook) GetRules() []string {
	names := make([]string, 0, h.RuleCount())
	h.rules.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// RuleCount 返回规则数量
func (h *Hook) RuleCount() int {
	return int(atomic.LoadInt64(&h.ruleCount))
}

// Clear 清空所有规则
func (h *Hook) Clear() {
	h.rules = sync.Map{}
	atomic.StoreInt64(&h.ruleCount, 0)
}

// Desensitize 对字符串进行脱敏处理
func (h *Hook) Desensitize(s string) string {
	if s == "" {
		return s
	}

	// 快速路径：无规则时直接返回
	if h.RuleCount() == 0 {
		return s
	}

	// 收集所有启用的规则
	var rules []Rule
	h.rules.Range(func(_, value any) bool {
		if rule, ok := value.(Rule); ok && rule.Enabled() {
			rules = append(rules, rule)
		}
		return true
	})

	// 如果没有启用的规则，直接返回
	if len(rules) == 0 {
		return s
	}

	// 按顺序应用所有启用的规则
	result := s
	for _, rule := range rules {
		result = rule.Process(result)
	}

	return result
}
