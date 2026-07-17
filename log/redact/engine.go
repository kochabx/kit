package redact

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"
)

type contentMatcher struct {
	rx   *regexp.Regexp
	mask Mask
}

type plan struct {
	fields  map[string]Mask
	content []contentMatcher
}

type ruleEntry struct {
	rule    Rule
	enabled bool
}

// Redactor atomically publishes immutable execution plans. Reads are lock-free;
// rule updates are serialized and become visible as one complete snapshot.
type Redactor struct {
	mu      sync.RWMutex
	rules   []ruleEntry
	index   map[string]int
	current atomic.Pointer[plan]
}

// New validates and compiles the initial rules.
func New(rules ...Rule) (*Redactor, error) {
	r := &Redactor{index: make(map[string]int, len(rules))}
	for _, rule := range rules {
		if _, exists := r.index[rule.name]; exists {
			return nil, fmt.Errorf("redact: duplicate rule %q", rule.name)
		}
		r.index[rule.name] = len(r.rules)
		r.rules = append(r.rules, ruleEntry{rule: rule, enabled: true})
	}
	compiled, err := compile(r.rules)
	if err != nil {
		return nil, err
	}
	r.current.Store(compiled)
	return r, nil
}

func compile(entries []ruleEntry) (*plan, error) {
	compiled := &plan{fields: make(map[string]Mask)}
	for _, entry := range entries {
		rule := entry.rule
		if rule.name == "" {
			return nil, fmt.Errorf("redact: rule name cannot be empty")
		}
		if rule.mask == nil {
			return nil, fmt.Errorf("redact: rule %q has no mask", rule.name)
		}
		switch rule.kind {
		case fieldRule:
			if entry.enabled {
				compiled.fields[rule.name] = rule.mask
			}
		case contentRule:
			if rule.pattern == "" {
				return nil, fmt.Errorf("redact: rule %q has an empty pattern", rule.name)
			}
			rx, err := regexp.Compile(rule.pattern)
			if err != nil {
				return nil, fmt.Errorf("redact: compile rule %q: %w", rule.name, err)
			}
			if entry.enabled {
				compiled.content = append(compiled.content, contentMatcher{rx: rx, mask: rule.mask})
			}
		default:
			return nil, fmt.Errorf("redact: rule %q has an invalid kind", rule.name)
		}
	}
	return compiled, nil
}

// AddRule adds and enables a rule. Duplicate names are rejected.
func (r *Redactor) AddRule(rule Rule) error {
	return r.AddRules(rule)
}

// AddRules atomically adds and enables a set of rules.
func (r *Redactor) AddRules(rules ...Rule) error {
	if len(rules) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := append([]ruleEntry(nil), r.rules...)
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if _, exists := r.index[rule.name]; exists {
			return fmt.Errorf("redact: duplicate rule %q", rule.name)
		}
		if _, exists := seen[rule.name]; exists {
			return fmt.Errorf("redact: duplicate rule %q", rule.name)
		}
		seen[rule.name] = struct{}{}
		next = append(next, ruleEntry{rule: rule, enabled: true})
	}
	compiled, err := compile(next)
	if err != nil {
		return err
	}
	r.rules = next
	r.reindexLocked()
	r.current.Store(compiled)
	return nil
}

// RemoveRule atomically removes a rule.
func (r *Redactor) RemoveRule(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, exists := r.index[name]
	if !exists {
		return false
	}
	next := append([]ruleEntry(nil), r.rules[:i]...)
	next = append(next, r.rules[i+1:]...)
	compiled, _ := compile(next)
	r.rules = next
	r.reindexLocked()
	r.current.Store(compiled)
	return true
}

// EnableRule atomically enables a rule.
func (r *Redactor) EnableRule(name string) bool { return r.setEnabled(name, true) }

// DisableRule atomically disables a rule.
func (r *Redactor) DisableRule(name string) bool { return r.setEnabled(name, false) }

// IsEnabled reports whether a rule exists and is enabled.
func (r *Redactor) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, exists := r.index[name]
	return exists && r.rules[i].enabled
}

func (r *Redactor) setEnabled(name string, enabled bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, exists := r.index[name]
	if !exists {
		return false
	}
	if r.rules[i].enabled == enabled {
		return true
	}
	next := append([]ruleEntry(nil), r.rules...)
	next[i].enabled = enabled
	compiled, _ := compile(next)
	r.rules = next
	r.current.Store(compiled)
	return true
}

func (r *Redactor) reindexLocked() {
	r.index = make(map[string]int, len(r.rules))
	for i, entry := range r.rules {
		r.index[entry.rule.name] = i
	}
}

// HasRules reports whether the engine contains any rules.
func (r *Redactor) HasRules() bool {
	if r == nil {
		return false
	}
	compiled := r.current.Load()
	return compiled != nil && (len(compiled.fields) != 0 || len(compiled.content) != 0)
}

// Append appends redacted src to dst. When src is unchanged, it returns dst
// unchanged and changed=false.
func (r *Redactor) Append(dst, src []byte) ([]byte, bool) {
	if r == nil || len(src) == 0 {
		return dst, false
	}

	compiled := r.current.Load()
	if compiled == nil || (len(compiled.fields) == 0 && len(compiled.content) == 0) {
		return dst, false
	}
	base := len(dst)
	result, fieldChanged := redactFields(compiled.fields, dst, src)
	input := src
	if fieldChanged {
		input = result[base:]
	}
	contentChanged := false
	for _, matcher := range compiled.content {
		text := unsafe.String(unsafe.SliceData(input), len(input))
		if !matcher.rx.MatchString(text) {
			continue
		}
		input = matcher.rx.ReplaceAllFunc(input, func(match []byte) []byte {
			return matcher.mask.Append(make([]byte, 0, len(match)), match)
		})
		contentChanged = true
	}
	if !fieldChanged && !contentChanged {
		return dst, false
	}
	if !contentChanged {
		return result, true
	}
	return append(result[:base], input...), true
}

// RedactString redacts a string for non-hot-path callers.
func (r *Redactor) RedactString(src string) string {
	result, changed := r.Append(nil, []byte(src))
	if !changed {
		return src
	}
	return string(result)
}

func redactFields(fields map[string]Mask, dst, src []byte) ([]byte, bool) {
	if len(fields) == 0 {
		return dst, false
	}
	out := dst
	changed := false
	last := 0
	for i := 0; i < len(src); {
		if src[i] != '"' {
			i++
			continue
		}
		end := stringEnd(src, i)
		if end < 0 {
			break
		}
		colon := skipSpace(src, end+1)
		if colon >= len(src) || src[colon] != ':' {
			i = end + 1
			continue
		}
		name, ok := jsonString(src[i : end+1])
		if !ok {
			i = end + 1
			continue
		}
		mask, exists := fields[name]
		valueStart := skipSpace(src, colon+1)
		valueEnd := jsonValueEnd(src, valueStart)
		if valueEnd < 0 {
			break
		}
		if !exists {
			i = valueEnd
			continue
		}

		value := src[valueStart:valueEnd]
		if len(value) >= 2 && value[0] == '"' {
			if bytes.IndexByte(value, '\\') < 0 {
				value = value[1 : len(value)-1]
			} else if decoded, valid := jsonString(value); valid {
				value = []byte(decoded)
			}
		}
		if !changed {
			out = append(out, src[:valueStart]...)
		} else {
			out = append(out, src[last:valueStart]...)
		}
		masked := mask.Append(nil, value)
		out = strconv.AppendQuote(out, string(masked))
		last = valueEnd
		changed = true
		i = valueEnd
	}
	if !changed {
		return dst, false
	}
	out = append(out, src[last:]...)
	return out, true
}

func jsonString(src []byte) (string, bool) {
	if bytes.IndexByte(src, '\\') < 0 {
		value := src[1 : len(src)-1]
		return unsafe.String(unsafe.SliceData(value), len(value)), true
	}
	value, err := strconv.Unquote(string(src))
	return value, err == nil
}

func stringEnd(src []byte, start int) int {
	escaped := false
	for i := start + 1; i < len(src); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch src[i] {
		case '\\':
			escaped = true
		case '"':
			return i
		}
	}
	return -1
}

func skipSpace(src []byte, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r' || src[i] == '\n') {
		i++
	}
	return i
}

func jsonValueEnd(src []byte, start int) int {
	if start >= len(src) {
		return -1
	}
	if src[start] == '"' {
		end := stringEnd(src, start)
		if end < 0 {
			return -1
		}
		return end + 1
	}
	if src[start] == '{' || src[start] == '[' {
		open, close := src[start], byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 1
		for i := start + 1; i < len(src); i++ {
			if src[i] == '"' {
				end := stringEnd(src, i)
				if end < 0 {
					return -1
				}
				i = end
				continue
			}
			if src[i] == open {
				depth++
			}
			if src[i] == close {
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
		return -1
	}
	for i := start; i < len(src); i++ {
		if src[i] == ',' || src[i] == '}' || src[i] == ']' || src[i] == '\n' || src[i] == '\r' {
			end := i
			for end > start && (src[end-1] == ' ' || src[end-1] == '\t') {
				end--
			}
			return end
		}
	}
	return len(src)
}
