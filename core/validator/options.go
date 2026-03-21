package validator

// Option 配置由 New 创建的 Validator。
type Option func(*config)

type config struct {
	defaultLang  Lang
	enabledLangs []Lang
	fieldNameTag string
}

func defaultConfig() *config {
	return &config{
		defaultLang:  LangEn,
		enabledLangs: []Lang{LangEn, LangZh},
		fieldNameTag: "json",
	}
}

// WithDefaultLang 设置未在 context 中指定语言时所用的默认语言。
// 默认值：LangEn。
func WithDefaultLang(lang Lang) Option {
	return func(c *config) {
		c.defaultLang = lang
	}
}

// WithLangs 替换已启用的翻译语言集合。
// 默认值：[LangEn, LangZh]。
func WithLangs(langs ...Lang) Option {
	return func(c *config) {
		c.enabledLangs = langs
	}
}

// WithFieldNameTag 指定用于提取字段名的结构体 tag（例如 "json"、"yaml"）。
// tag 值的首个逗号前部分将作为字段名出现在错误消息中。
// 传入空字符串则使用 Go 结构体字段名。
// 默认值："json"。
func WithFieldNameTag(tag string) Option {
	return func(c *config) {
		c.fieldNameTag = tag
	}
}
