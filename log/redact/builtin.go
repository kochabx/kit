package redact

// BuiltinRules returns conservative defaults for common sensitive data.
func BuiltinRules() []Rule {
	return []Rule{
		Field("password", Replace("******")),
		Field("token", Replace("******")),
		Field("secret", Replace("******")),
		Field("phone", KeepEdges(3, 4)),
		Field("idcard", KeepEdges(6, 4)),
		Field("bankcard", KeepEdges(4, 4)),
		Content("phone-content", `1[3-9]\d{9}`, KeepEdges(3, 4)),
		Content("email-content", `\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, Email()),
	}
}
