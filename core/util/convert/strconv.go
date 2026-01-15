package convert

import (
	"fmt"
	"strconv"
)

// Number 是所有支持的数值类型的约束接口
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// ParseStrings 将字符串切片转换为指定数值类型的切片
// 支持的类型由 Number 约束定义
func ParseStrings[T Number](strings []string) ([]T, error) {
	if len(strings) == 0 {
		return []T{}, nil
	}

	result := make([]T, 0, len(strings))
	for i, s := range strings {
		val, err := parseString[T](s)
		if err != nil {
			return nil, fmt.Errorf("parse index %d (%q): %w", i, s, err)
		}
		result = append(result, val)
	}
	return result, nil
}

// parseString 解析单个字符串为指定数值类型
func parseString[T Number](s string) (T, error) {
	var zero T
	switch any(zero).(type) {
	case int:
		val, err := strconv.Atoi(s)
		return T(val), err
	case int8:
		val, err := strconv.ParseInt(s, 10, 8)
		return T(val), err
	case int16:
		val, err := strconv.ParseInt(s, 10, 16)
		return T(val), err
	case int32:
		val, err := strconv.ParseInt(s, 10, 32)
		return T(val), err
	case int64:
		val, err := strconv.ParseInt(s, 10, 64)
		return T(val), err
	case uint:
		val, err := strconv.ParseUint(s, 10, 0)
		return T(val), err
	case uint8:
		val, err := strconv.ParseUint(s, 10, 8)
		return T(val), err
	case uint16:
		val, err := strconv.ParseUint(s, 10, 16)
		return T(val), err
	case uint32:
		val, err := strconv.ParseUint(s, 10, 32)
		return T(val), err
	case uint64:
		val, err := strconv.ParseUint(s, 10, 64)
		return T(val), err
	case float32:
		val, err := strconv.ParseFloat(s, 32)
		return T(val), err
	case float64:
		val, err := strconv.ParseFloat(s, 64)
		return T(val), err
	default:
		return zero, fmt.Errorf("unsupported type: %T", zero)
	}
}

// MustParseStrings 类似 ParseStrings，但在解析失败时会 panic
func MustParseStrings[T Number](strings []string) []T {
	result, err := ParseStrings[T](strings)
	if err != nil {
		panic(err)
	}
	return result
}

// ParseString 解析单个字符串为指定数值类型
func ParseString[T Number](s string) (T, error) {
	return parseString[T](s)
}

// MustParseString 类似 ParseString，但在解析失败时会 panic
func MustParseString[T Number](s string) T {
	result, err := parseString[T](s)
	if err != nil {
		panic(err)
	}
	return result
}
