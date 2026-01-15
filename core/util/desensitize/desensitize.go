package desensitize

import "strings"

// Mobile 手机号脱敏，保留前3位和后3位
// 例如：13812345678 -> 138****5678
func Mobile(mobile string) string {
	length := len(mobile)
	if length < 7 {
		return mobile
	}
	return mobile[:3] + "****" + mobile[length-3:]
}

// Email 邮箱脱敏，保留前3位和@后的内容
// 例如：test@example.com -> tes****@example.com
func Email(email string) string {
	index := strings.IndexByte(email, '@')
	if index == -1 || index < 4 {
		return email
	}
	return email[:3] + "****" + email[index:]
}

// IDCard 身份证号脱敏，保留前4位和后4位
// 例如：110101199001011234 -> 1101********1234
func IDCard(idCard string) string {
	length := len(idCard)
	if length < 8 {
		return idCard
	}
	return idCard[:4] + strings.Repeat("*", length-8) + idCard[length-4:]
}

// BankCard 银行卡号脱敏，保留前4位和后4位
// 例如：6222021234567890123 -> 6222***********0123
func BankCard(cardNumber string) string {
	length := len(cardNumber)
	if length < 8 {
		return cardNumber
	}
	return cardNumber[:4] + strings.Repeat("*", length-8) + cardNumber[length-4:]
}

// Name 姓名脱敏，保留姓氏
// 例如：张三 -> 张*，张三丰 -> 张**
func Name(name string) string {
	runes := []rune(name)
	length := len(runes)
	if length <= 1 {
		return name
	}
	return string(runes[0]) + strings.Repeat("*", length-1)
}

// Address 地址脱敏，保留前6个字符
// 例如：北京市朝阳区某某街道123号 -> 北京市朝阳区******
func Address(address string) string {
	runes := []rune(address)
	length := len(runes)
	if length <= 6 {
		return address
	}
	return string(runes[:6]) + strings.Repeat("*", length-6)
}

// Custom 自定义脱敏，保留前 keep 位和后 keep 位
func Custom(s string, keep int) string {
	length := len(s)
	if length <= keep*2 {
		return s
	}
	return s[:keep] + strings.Repeat("*", length-keep*2) + s[length-keep:]
}
