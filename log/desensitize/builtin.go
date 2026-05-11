package desensitize

var (
	// PhoneRule 手机号脱敏规则 (13812345678 -> 138****5678)
	PhoneRule, _ = NewContentRule(
		"phone",
		`(1[3-9]\d)\d{4}(\d{4})`,
		"$1****$2",
	)

	// EmailRule 邮箱脱敏规则 (user@example.com -> u***r@e***.com)
	EmailRule, _ = NewContentRule(
		"email",
		`\b([A-Za-z0-9])[A-Za-z0-9._%+-]*([A-Za-z0-9])@([A-Za-z0-9])[A-Za-z0-9.-]*\.([A-Z|a-z]{2,})\b`,
		"$1***$2@$3***.$4",
	)

	// IDCardRule 身份证号脱敏规则 (保留前6位和后4位)
	IDCardRule, _ = NewContentRule(
		"idcard",
		`\b(\d{6})\d{8}(\d{4})\b`,
		"$1********$2",
	)

	// BankCardRule 银行卡号脱敏规则 (保留前4位和后4位)
	BankCardRule, _ = NewContentRule(
		"bankcard",
		`\b(\d{4})\d{8,11}(\d{4})\b`,
		"$1 **** **** $2",
	)

	// PasswordRule 密码字段脱敏规则（针对JSON中的password字段）
	PasswordRule, _ = NewFieldRule(
		"password",
		"password",
		`.*`,
		"******",
	)

	// TokenRule Token字段脱敏规则
	TokenRule, _ = NewFieldRule(
		"token",
		"token",
		`.*`,
		"******",
	)

	// SecretRule Secret字段脱敏规则
	SecretRule, _ = NewFieldRule(
		"secret",
		"secret",
		`.*`,
		"******",
	)
)

// BuiltinRules 返回所有内置规则
func BuiltinRules() []Rule {
	return []Rule{
		IDCardRule,
		BankCardRule,
		PasswordRule,
		TokenRule,
		SecretRule,
	}
}
