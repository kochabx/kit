package redact

// Rule is compiled by New into an immutable redaction plan.
type Rule struct {
	kind    ruleKind
	name    string
	pattern string
	mask    Mask
}

type ruleKind uint8

const (
	fieldRule ruleKind = iota
	contentRule
)

// Field masks a JSON field by its exact name.
func Field(name string, mask Mask) Rule {
	return Rule{kind: fieldRule, name: name, mask: mask}
}

// Content masks text matched by a regular expression.
func Content(name, pattern string, mask Mask) Rule {
	return Rule{kind: contentRule, name: name, pattern: pattern, mask: mask}
}
