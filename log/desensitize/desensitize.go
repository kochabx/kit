package desensitize

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// snapshot 预计算的只读快照，Desensitize 通过原子指针无锁读取
type snapshot struct {
	contentRx    *regexp.Regexp
	contentRules []*ContentRule
	fieldRx      *regexp.Regexp
	fieldMap     map[string]*FieldRule
}

// Hook 脱敏钩子
type Hook struct {
	mu        sync.Mutex
	rules     []Rule
	index     map[string]int
	disabled  map[string]bool
	ruleCount atomic.Int64
	snap      atomic.Pointer[snapshot]
}

// NewHook 创建脱敏钩子
func NewHook() *Hook {
	return &Hook{
		index:    make(map[string]int),
		disabled: make(map[string]bool),
	}
}

// rebuildLocked 重建只读快照，必须在持锁时调用
func (h *Hook) rebuildLocked() {
	if len(h.rules) == 0 {
		h.snap.Store(nil)
		return
	}

	var crs []*ContentRule
	fm := make(map[string]*FieldRule)
	for _, r := range h.rules {
		if h.disabled[r.Name()] {
			continue
		}
		switch t := r.(type) {
		case *ContentRule:
			crs = append(crs, t)
		case *FieldRule:
			fm[t.fieldName] = t
		}
	}

	snap := &snapshot{contentRules: crs, fieldMap: fm}

	// 合并 ContentRule: (?:pat0)|(?:pat1)|...
	if len(crs) > 0 {
		var sb strings.Builder
		for i, cr := range crs {
			if i > 0 {
				sb.WriteByte('|')
			}
			sb.WriteString("(?:")
			sb.WriteString(cr.pattern.String())
			sb.WriteByte(')')
		}
		snap.contentRx = regexp.MustCompile(sb.String())
	}

	// 合并 FieldRule: "(field1|field2|...)"\s*:\s*"([^"]*)"
	if len(fm) > 0 {
		var sb strings.Builder
		sb.WriteString(`"(`)
		first := true
		for name := range fm {
			if !first {
				sb.WriteByte('|')
			}
			sb.WriteString(regexp.QuoteMeta(name))
			first = false
		}
		sb.WriteString(`)"\s*:\s*"([^"]*)"`)
		snap.fieldRx = regexp.MustCompile(sb.String())
	}

	h.snap.Store(snap)
}

// AddRule 添加脱敏规则（同名覆盖）
func (h *Hook) AddRule(rule Rule) {
	if rule == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if i, ok := h.index[rule.Name()]; ok {
		h.rules[i] = rule
	} else {
		h.index[rule.Name()] = len(h.rules)
		h.rules = append(h.rules, rule)
		h.ruleCount.Add(1)
	}
	h.rebuildLocked()
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
	h.mu.Lock()
	defer h.mu.Unlock()
	i, ok := h.index[name]
	if !ok {
		return false
	}
	last := len(h.rules) - 1
	if i != last {
		h.rules[i] = h.rules[last]
		h.index[h.rules[i].Name()] = i
	}
	h.rules[last] = nil
	h.rules = h.rules[:last]
	delete(h.index, name)
	delete(h.disabled, name)
	h.ruleCount.Add(-1)
	h.rebuildLocked()
	return true
}

// EnableRule 启用规则
func (h *Hook) EnableRule(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.index[name]; ok {
		delete(h.disabled, name)
		h.rebuildLocked()
		return true
	}
	return false
}

// DisableRule 禁用规则
func (h *Hook) DisableRule(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.index[name]; ok {
		h.disabled[name] = true
		h.rebuildLocked()
		return true
	}
	return false
}

// IsEnabled 返回规则是否启用
func (h *Hook) IsEnabled(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, exists := h.index[name]
	return exists && !h.disabled[name]
}

// GetRule 获取指定规则
func (h *Hook) GetRule(name string) (Rule, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i, ok := h.index[name]; ok {
		return h.rules[i], true
	}
	return nil, false
}

// GetRules 列出所有规则名称
func (h *Hook) GetRules() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	names := make([]string, len(h.rules))
	for i, r := range h.rules {
		names[i] = r.Name()
	}
	return names
}

// RuleCount 返回规则总数
func (h *Hook) RuleCount() int {
	return int(h.ruleCount.Load())
}

// Clear 清空所有规则
func (h *Hook) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rules = nil
	h.index = make(map[string]int)
	h.disabled = make(map[string]bool)
	h.ruleCount.Store(0)
	h.snap.Store(nil)
}

// Desensitize 对文本进行脱敏处理
func (h *Hook) Desensitize(s string) string {
	if s == "" {
		return s
	}
	snap := h.snap.Load()
	if snap == nil {
		return s
	}

	result := s

	// 所有 ContentRule 合并为单次正则扫描
	if snap.contentRx != nil {
		result = snap.contentRx.ReplaceAllStringFunc(result, func(match string) string {
			for _, cr := range snap.contentRules {
				if cr.pattern.MatchString(match) {
					return cr.pattern.ReplaceAllString(match, cr.replacement)
				}
			}
			return match
		})
	}

	// 所有 FieldRule 合并为单次正则扫描
	if snap.fieldRx != nil {
		result = replaceFields(result, snap.fieldRx, snap.fieldMap)
	}

	return result
}

// replaceFields 单次扫描替换所有 JSON 字段值
func replaceFields(s string, rx *regexp.Regexp, fm map[string]*FieldRule) string {
	locs := rx.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s
	}
	var buf strings.Builder
	buf.Grow(len(s))
	prev := 0
	for _, loc := range locs {
		name := s[loc[2]:loc[3]]
		value := s[loc[4]:loc[5]]
		fr := fm[name]
		newValue := fr.fieldPattern.ReplaceAllString(value, fr.replacement)
		buf.WriteString(s[prev:loc[0]])
		buf.WriteByte('"')
		buf.WriteString(name)
		buf.WriteString(`":"`)
		buf.WriteString(newValue)
		buf.WriteByte('"')
		prev = loc[1]
	}
	buf.WriteString(s[prev:])
	return buf.String()
}
