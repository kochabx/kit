package validator

// Locale identifies the language used for validation messages.
type Locale string

const (
	// LocaleEN selects English and is the default fallback locale.
	LocaleEN Locale = "en"
	// LocaleZH selects Simplified Chinese.
	LocaleZH Locale = "zh"
)
