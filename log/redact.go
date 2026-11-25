package log

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mask transforms a matched sensitive string before logging.
type Mask interface {
	Apply(value string) string
}

type maskFunc func(string) string

func (f maskFunc) Apply(value string) string { return f(value) }

// MaskFull replaces the entire value with replacement (default "******").
func MaskFull(replacement ...string) Mask {
	repl := "******"
	if len(replacement) > 0 && replacement[0] != "" {
		repl = replacement[0]
	}
	return maskFunc(func(string) string { return repl })
}

// MaskPartial preserves prefix and suffix runes and masks the middle.
func MaskPartial(prefix, suffix int) Mask {
	prefix = max(prefix, 0)
	suffix = max(suffix, 0)
	return maskFunc(func(value string) string {
		runes := []rune(value)
		if prefix >= len(runes) || suffix >= len(runes)-prefix {
			return "******"
		}
		return string(runes[:prefix]) + "****" + string(runes[len(runes)-suffix:])
	})
}

// MaskEmail preserves email domain and edge runes of the local part.
func MaskEmail() Mask {
	return maskFunc(func(value string) string {
		at := strings.LastIndexByte(value, '@')
		if at <= 0 || at == len(value)-1 {
			return "******"
		}
		localRunes, rest := []rune(value[:at]), value[at:]
		if len(localRunes) == 1 {
			return string(localRunes[:1]) + "***" + rest
		}
		return string(localRunes[:1]) + "***" + string(localRunes[len(localRunes)-1:]) + rest
	})
}

type ruleKind uint8

const (
	fieldRule ruleKind = iota
	contentRule
)

// Rule defines either a field-name rule or a regex content redaction rule.
type Rule struct {
	kind    ruleKind
	name    string
	pattern string
	mask    Mask
}

// FieldRule masks any slog attribute matching name (case-insensitive) at any Group depth.
func FieldRule(name string, mask Mask) Rule {
	return Rule{kind: fieldRule, name: name, mask: mask}
}

// ContentRule masks regex matches inside log messages and string attributes.
func ContentRule(name, pattern string, mask Mask) Rule {
	return Rule{kind: contentRule, name: name, pattern: pattern, mask: mask}
}

type contentMatcher struct {
	rx   *regexp.Regexp
	mask Mask
}

type redactPlan struct {
	fields  map[string]Mask
	content []contentMatcher
}

type ruleEntry struct {
	rule    Rule
	enabled bool
}

// Redactor atomically compiles and publishes immutable redaction plans. Reads on
// the log hot path are lock-free; rule mutations are serialized and atomic.
type Redactor struct {
	mu      sync.Mutex
	rules   []ruleEntry
	index   map[string]int
	current atomic.Pointer[redactPlan]
}

// NewRedactor validates and compiles initial rules.
func NewRedactor(rules ...Rule) (*Redactor, error) {
	r := &Redactor{index: make(map[string]int, len(rules))}
	if err := r.AddRules(rules...); err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		r.current.Store(&redactPlan{fields: make(map[string]Mask)})
	}
	return r, nil
}

func ruleIndexKey(name string) string {
	return strings.ToLower(name)
}

func (r *Redactor) lookupRuleIndexLocked(name string) (int, bool) {
	idx, ok := r.index[ruleIndexKey(name)]
	return idx, ok
}

// AddRule adds and enables a single rule. Duplicate names are rejected.
func (r *Redactor) AddRule(rule Rule) error { return r.AddRules(rule) }

// AddRules atomically adds and enables rules.
func (r *Redactor) AddRules(rules ...Rule) error {
	if len(rules) == 0 && r.current.Load() != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.index == nil {
		r.index = make(map[string]int, len(rules))
	}

	next := slices.Clone(r.rules)
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		key := ruleIndexKey(rule.name)
		if _, exists := r.index[key]; exists {
			return fmt.Errorf("log: duplicate redaction rule %q", rule.name)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("log: duplicate redaction rule %q", rule.name)
		}
		seen[key] = struct{}{}
		next = append(next, ruleEntry{rule: rule, enabled: true})
	}
	compiled, err := compilePlan(next)
	if err != nil {
		return err
	}
	r.rules = next
	r.reindexLocked()
	r.current.Store(compiled)
	return nil
}

// RemoveRule removes a named rule and atomically updates the active plan.
func (r *Redactor) RemoveRule(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx, ok := r.lookupRuleIndexLocked(name)
	if !ok {
		return false
	}
	next := slices.Delete(slices.Clone(r.rules), idx, idx+1)
	compiled, err := compilePlan(next)
	if err != nil {
		return false
	}
	r.rules = next
	r.reindexLocked()
	r.current.Store(compiled)
	return true
}

// EnableRule enables a named rule. Returns false if the rule does not exist.
func (r *Redactor) EnableRule(name string) bool { return r.setEnabled(name, true) }

// DisableRule disables a named rule. Returns false if the rule does not exist.
func (r *Redactor) DisableRule(name string) bool { return r.setEnabled(name, false) }

func (r *Redactor) setEnabled(name string, enabled bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx, ok := r.lookupRuleIndexLocked(name)
	if !ok {
		return false
	}
	if r.rules[idx].enabled == enabled {
		return true
	}
	next := slices.Clone(r.rules)
	next[idx].enabled = enabled
	compiled, err := compilePlan(next)
	if err != nil {
		return false
	}
	r.rules = next
	r.current.Store(compiled)
	return true
}

func (r *Redactor) reindexLocked() {
	clear(r.index)
	for i, entry := range r.rules {
		r.index[ruleIndexKey(entry.rule.name)] = i
	}
}

func compilePlan(entries []ruleEntry) (*redactPlan, error) {
	plan := &redactPlan{fields: make(map[string]Mask)}
	for _, entry := range entries {
		rule := entry.rule
		if rule.name == "" {
			return nil, fmt.Errorf("log: redaction rule name cannot be empty")
		}
		if rule.mask == nil {
			return nil, fmt.Errorf("log: redaction rule %q has nil mask", rule.name)
		}
		switch rule.kind {
		case fieldRule:
			if entry.enabled {
				plan.fields[strings.ToLower(rule.name)] = rule.mask
			}
		case contentRule:
			if rule.pattern == "" {
				return nil, fmt.Errorf("log: content rule %q has empty pattern", rule.name)
			}
			rx, err := regexp.Compile(rule.pattern)
			if err != nil {
				return nil, fmt.Errorf("log: compile content rule %q: %w", rule.name, err)
			}
			if entry.enabled {
				plan.content = append(plan.content, contentMatcher{rx: rx, mask: rule.mask})
			}
		default:
			return nil, fmt.Errorf("log: invalid rule kind for %q", rule.name)
		}
	}
	return plan, nil
}

// Handler wraps next in a slog.Handler applying this Redactor's dynamic plan.
func (r *Redactor) Handler(next slog.Handler) slog.Handler {
	if r == nil || next == nil {
		return next
	}
	return &redactHandler{redactor: r, next: next}
}

type handlerFrame struct {
	group string
	attrs []slog.Attr
}

type redactHandler struct {
	redactor *Redactor
	next     slog.Handler
	frames   []handlerFrame
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	frames := append(slices.Clone(h.frames), handlerFrame{group: name})
	return &redactHandler{redactor: h.redactor, next: h.next, frames: frames}
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	frames := append(slices.Clone(h.frames), handlerFrame{attrs: slices.Clone(attrs)})
	return &redactHandler{redactor: h.redactor, next: h.next, frames: frames}
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	plan := h.redactor.current.Load()
	hasRules := plan != nil && (len(plan.fields) > 0 || len(plan.content) > 0)
	if !hasRules && len(h.frames) == 0 {
		return h.next.Handle(ctx, rec)
	}

	attrs := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	for _, frame := range slices.Backward(h.frames) {

		if frame.group != "" {
			if len(attrs) > 0 {
				attrs = []slog.Attr{{Key: frame.group, Value: slog.GroupValue(attrs...)}}
			}
		} else if len(frame.attrs) > 0 {
			merged := make([]slog.Attr, 0, len(frame.attrs)+len(attrs))
			merged = append(merged, frame.attrs...)
			merged = append(merged, attrs...)
			attrs = merged
		}
	}

	msg := rec.Message
	if hasRules {
		msg = redactString(plan, msg)
		attrs = redactAttrSlice(plan, attrs)
	}
	out := slog.NewRecord(rec.Time, rec.Level, msg, rec.PC)
	out.AddAttrs(attrs...)
	return h.next.Handle(ctx, out)
}

func redactAttrSlice(plan *redactPlan, attrs []slog.Attr) []slog.Attr {
	if plan == nil || (len(plan.fields) == 0 && len(plan.content) == 0) {
		return attrs
	}
	out := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		out[i] = redactAttr(plan, attr)
	}
	return out
}

func redactAttr(plan *redactPlan, attr slog.Attr) slog.Attr {
	val := attr.Value.Resolve()
	if attr.Key != "" {
		if mask, ok := plan.fields[strings.ToLower(attr.Key)]; ok {
			return slog.String(attr.Key, mask.Apply(valueString(val)))
		}
	}
	switch val.Kind() {
	case slog.KindGroup:
		groupAttrs := val.Group()
		masked := make([]slog.Attr, len(groupAttrs))
		for i, child := range groupAttrs {
			masked[i] = redactAttr(plan, child)
		}
		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(masked...)}
	case slog.KindString:
		masked := redactString(plan, val.String())
		if masked != val.String() {
			return slog.String(attr.Key, masked)
		}
	case slog.KindAny:
		if redacted, changed := redactAnyValue(plan, val.Any(), 0, nil); changed {
			return slog.Any(attr.Key, redacted)
		}
	}
	return slog.Attr{Key: attr.Key, Value: val}
}

func valueString(val slog.Value) string {
	if val.Kind() == slog.KindAny {
		if err, ok := val.Any().(error); ok && err != nil {
			return err.Error()
		}
	}
	return val.String()
}

const (
	maxRedactDepth   = 16
	redactedMaxDepth = "[REDACTED:MAX_DEPTH]"
)

func redactAnyValue(plan *redactPlan, v any, depth int, seen map[uintptr]struct{}) (any, bool) {
	if v == nil {
		return nil, false
	}
	if depth > maxRedactDepth {
		return redactedMaxDepth, true
	}
	switch x := v.(type) {
	case string:
		masked := redactString(plan, x)
		return masked, masked != x
	case error:
		if x == nil {
			return v, false
		}
		s := x.Error()
		masked := redactString(plan, s)
		return masked, masked != s
	case []byte:
		s := string(x)
		masked := redactString(plan, s)
		return masked, masked != s
	case []string:
		var out []string
		for i, s := range x {
			if masked := redactString(plan, s); masked != s {
				if out == nil {
					out = slices.Clone(x)
				}
				out[i] = masked
			}
		}
		if out != nil {
			return out, true
		}
		return v, false
	case map[string]string:
		var out map[string]string
		for k, s := range x {
			maskedVal := redactString(plan, s)
			if mask, ok := plan.fields[strings.ToLower(k)]; ok {
				maskedVal = mask.Apply(s)
			}
			if maskedVal != s {
				if out == nil {
					out = make(map[string]string, len(x))
					maps.Copy(out, x)
				}
				out[k] = maskedVal
			}
		}
		if out != nil {
			return out, true
		}
		return v, false
	case map[string]any:
		return redactMapStringAny(plan, x, depth+1, seen)
	case []any:
		var out []any
		for i, elem := range x {
			if redacted, changed := redactAnyValue(plan, elem, depth+1, seen); changed {
				if out == nil {
					out = slices.Clone(x)
				}
				out[i] = redacted
			}
		}
		if out != nil {
			return out, true
		}
		return v, false
	case time.Time, time.Duration:
		return v, false
	}

	rv := reflect.ValueOf(v)
	return redactReflect(plan, v, rv, depth, seen)
}

func redactMapStringAny(plan *redactPlan, m map[string]any, depth int, seen map[uintptr]struct{}) (any, bool) {
	if depth > maxRedactDepth {
		return redactedMaxDepth, true
	}
	var out map[string]any
	ensureCopy := func() {
		if out == nil {
			out = make(map[string]any, len(m))
			maps.Copy(out, m)
		}
	}
	for k, val := range m {
		if mask, ok := plan.fields[strings.ToLower(k)]; ok {
			ensureCopy()
			out[k] = mask.Apply(fmt.Sprint(val))
			continue
		}
		if redacted, changed := redactAnyValue(plan, val, depth+1, seen); changed {
			ensureCopy()
			out[k] = redacted
		}
	}
	if out != nil {
		return out, true
	}
	return m, false
}

func redactReflect(plan *redactPlan, orig any, rv reflect.Value, depth int, seen map[uintptr]struct{}) (any, bool) {
	if depth > maxRedactDepth {
		return redactedMaxDepth, true
	}
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return orig, false
		}
		if rv.Kind() == reflect.Pointer {
			ptr := rv.Pointer()
			if seen == nil {
				seen = make(map[uintptr]struct{}, 4)
			}
			if _, ok := seen[ptr]; ok {
				return orig, false
			}
			seen[ptr] = struct{}{}
			defer delete(seen, ptr)
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.String:
		s := rv.String()
		if masked := redactString(plan, s); masked != s {
			return masked, true
		}
		return orig, false
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			return redactStringMap(plan, orig, rv, depth, seen)
		}
		return redactGenericMap(plan, orig, rv, depth, seen)
	case reflect.Slice, reflect.Array:
		return redactSliceOrArray(plan, orig, rv, depth, seen)
	case reflect.Struct:
		return redactStruct(plan, orig, rv, depth, seen)
	default:
		if str, ok := orig.(fmt.Stringer); ok {
			s := str.String()
			if masked := redactString(plan, s); masked != s {
				return masked, true
			}
		}
		return orig, false
	}
}

func redactStringMap(plan *redactPlan, orig any, rv reflect.Value, depth int, seen map[uintptr]struct{}) (any, bool) {
	var out map[string]any
	ensureMap := func() {
		if out != nil {
			return
		}
		out = make(map[string]any, rv.Len())
		for _, mk := range rv.MapKeys() {
			out[mk.String()] = rv.MapIndex(mk).Interface()
		}
	}
	for _, mk := range rv.MapKeys() {
		k := mk.String()
		mv := rv.MapIndex(mk).Interface()
		if mask, ok := plan.fields[strings.ToLower(k)]; ok {
			ensureMap()
			out[k] = mask.Apply(fmt.Sprint(mv))
			continue
		}
		if redacted, changed := redactAnyValue(plan, mv, depth+1, seen); changed {
			ensureMap()
			out[k] = redacted
		}
	}
	if out != nil {
		return out, true
	}
	return orig, false
}

func redactGenericMap(plan *redactPlan, orig any, rv reflect.Value, depth int, seen map[uintptr]struct{}) (any, bool) {
	var out reflect.Value
	iter := rv.MapRange()
	for iter.Next() {
		k := iter.Key()
		redacted, changed := redactAnyValue(plan, iter.Value().Interface(), depth+1, seen)
		if !changed {
			continue
		}
		if !out.IsValid() {
			out = reflect.MakeMapWithSize(reflect.MapOf(rv.Type().Key(), reflect.TypeFor[any]()), rv.Len())
			cp := rv.MapRange()
			for cp.Next() {
				out.SetMapIndex(cp.Key(), cp.Value())
			}
		}
		if redacted == nil {
			out.SetMapIndex(k, reflect.Zero(reflect.TypeFor[any]()))
		} else {
			out.SetMapIndex(k, reflect.ValueOf(redacted))
		}
	}
	if out.IsValid() {
		return out.Interface(), true
	}
	return orig, false
}

func redactSliceOrArray(plan *redactPlan, orig any, rv reflect.Value, depth int, seen map[uintptr]struct{}) (any, bool) {
	out := make([]any, rv.Len())
	changedAny := false
	for i := range rv.Len() {
		elem := rv.Index(i).Interface()
		if redacted, changed := redactAnyValue(plan, elem, depth+1, seen); changed {
			out[i] = redacted
			changedAny = true
		} else {
			out[i] = elem
		}
	}
	if changedAny {
		return out, true
	}
	return orig, false
}

func redactStruct(plan *redactPlan, orig any, rv reflect.Value, depth int, seen map[uintptr]struct{}) (any, bool) {
	rt := rv.Type()
	fields := make(map[string]any, rt.NumField())
	changedAny := false
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		keyName := sf.Name
		if tag, _, _ := strings.Cut(sf.Tag.Get("json"), ","); tag == "-" {
			continue
		} else if tag != "" {
			keyName = tag
		}
		fv := rv.Field(i).Interface()
		if mask, ok := plan.fields[strings.ToLower(keyName)]; ok {
			fields[keyName] = mask.Apply(fmt.Sprint(fv))
			changedAny = true
			continue
		}
		if keyName != sf.Name {
			if mask, ok := plan.fields[strings.ToLower(sf.Name)]; ok {
				fields[keyName] = mask.Apply(fmt.Sprint(fv))
				changedAny = true
				continue
			}
		}
		if redacted, changed := redactAnyValue(plan, fv, depth+1, seen); changed {
			fields[keyName] = redacted
			changedAny = true
		} else {
			fields[keyName] = fv
		}
	}
	if changedAny {
		return fields, true
	}
	if str, ok := orig.(fmt.Stringer); ok {
		s := str.String()
		if masked := redactString(plan, s); masked != s {
			return masked, true
		}
	}
	return orig, false
}

func redactString(plan *redactPlan, s string) string {
	if s == "" || len(plan.content) == 0 {
		return s
	}
	out := s
	for _, matcher := range plan.content {
		out = matcher.rx.ReplaceAllStringFunc(out, matcher.mask.Apply)
	}
	return out
}
