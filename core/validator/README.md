# validator

基于 [go-playground/validator](https://github.com/go-playground/validator) 的结构体 / 变量校验封装，提供多语言错误消息、结构化错误输出和函数式选项配置。

## 特性

- **接口抽象** — 通过 `Validator` 接口暴露 `Struct` 和 `Var` 两个方法，便于 mock 和替换
- **多语言翻译** — 内置 `en`、`zh` 翻译，可通过 `WithLocale` 扩展任意语言
- **动态语言切换** — 通过 `WithLocaleExtractor` 从 `context.Context` 提取语言，适配 HTTP 中间件场景
- **结构化错误** — 校验失败返回 `*ValidationError`，包含 `[]Violation`，每条记录字段名、标签、参数、值和翻译后的消息
- **字段名映射** — 默认从 `json` tag 提取字段名，可通过 `WithFieldNameTag` 切换或禁用
- **自定义校验** — 支持 tag 级别和 struct 级别的自定义校验函数注册
- **并发安全** — 同一 `Validator` 实例可跨 goroutine 共享使用

## 接口

```go
type Validator interface {
    Struct(ctx context.Context, s any) error
    Var(ctx context.Context, field any, tag string) error
}
```

## 错误结构

```go
type ValidationError struct { /* ... */ }
func (ve *ValidationError) Error() string        // "field1 msg; field2 msg"
func (ve *ValidationError) Violations() []Violation

type Violation struct {
    Field   string // 字段名（受 WithFieldNameTag 影响）
    Tag     string // 校验约束标签（如 "required"）
    Param   string // 约束参数（如 gte=18 中的 "18"）
    Value   any    // 校验时的字段值
    Message string // 翻译后的错误消息
}
```

## 使用示例

### 基础用法

```go
type User struct {
    Name  string `json:"name"  validate:"required"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age"   validate:"gte=18,lte=150"`
}

// 使用全局默认实例
err := validator.Validate.Struct(ctx, &User{Name: "", Email: "bad", Age: 10})

var ve *validator.ValidationError
if errors.As(err, &ve) {
    for _, v := range ve.Violations() {
        fmt.Printf("field=%s tag=%s param=%s msg=%s\n", v.Field, v.Tag, v.Param, v.Message)
    }
}
// field=name  tag=required param=     msg=name is a required field
// field=email tag=email    param=     msg=email must be a valid email address
// field=age   tag=gte      param=18   msg=age must be 18 or greater
```

### 自定义实例

```go
v, err := validator.New(
    validator.WithDefaultLocale(validator.LocaleZH),
    validator.WithFieldNameTag("json"),
    validator.WithLocaleExtractor(func(ctx context.Context) (validator.Locale, bool) {
        if lang, ok := ctx.Value(langKey{}).(validator.Locale); ok {
            return lang, true
        }
        return "", false
    }),
)
```

### 单值校验

```go
err := v.Var(ctx, "bad-email", "email")
if validator.AsValidationError(err) {
    // 校验失败
}
```

### 扩展语言

```go
import (
    "github.com/go-playground/locales/ja"
    ja_translations "github.com/go-playground/validator/v10/translations/ja"
)

const LocaleJA validator.Locale = "ja"

v, _ := validator.New(
    validator.WithLocale(LocaleJA, validator.LocaleEntry{
        Loc:      ja.New(),
        Register: ja_translations.RegisterDefaultTranslations,
    }),
    validator.WithDefaultLocale(LocaleJA),
)
```

### 自定义校验函数

```go
// tag 级别
v, _ := validator.New(
    validator.WithValidation("notempty", func(fl gv.FieldLevel) bool {
        return fl.Field().String() != ""
    }),
)

// struct 级别（跨字段校验）
type DateRange struct {
    Start int `validate:"required"`
    End   int `validate:"required"`
}

v, _ := validator.New(
    validator.WithStructValidation(func(sl gv.StructLevel) {
        dr := sl.Current().Interface().(DateRange)
        if dr.Start > dr.End {
            sl.ReportError(dr.End, "end", "End", "gtstart", "")
        }
    }, DateRange{}),
)
```

### 配合 Gin 使用

禁用 Gin 内置校验，单独调用 `Struct(ctx)` 走完整的 locale 链路：

```go
func init() {
    binding.Validator = nil // 禁用 Gin 自动校验
}

func CreateUser(c *gin.Context) {
    var req User
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": "invalid json"})
        return
    }
    if err := v.Struct(c.Request.Context(), &req); err != nil {
        var ve *validator.ValidationError
        if errors.As(err, &ve) {
            // 返回翻译后的消息
            c.JSON(422, gin.H{"errors": ve.Violations()})
            // 或返回结构化数据供前端 i18n
            // c.JSON(422, gin.H{"errors": ve.Violations()})
            // → [{"field":"age","tag":"gte","param":"18","message":"..."}]
            return
        }
    }
    // ...
}
```

## 选项

| 选项 | 说明 | 默认值 |
|---|---|---|
| `WithDefaultLocale(locale)` | 回退语言 | `LocaleEN` |
| `WithLocale(locale, entry)` | 注册 / 覆盖语言翻译 | 内置 `en`、`zh` |
| `WithFieldNameTag(tag)` | 字段名来源的 struct tag，空字符串使用 Go 字段名 | `"json"` |
| `WithLocaleExtractor(fn)` | 从 context 提取语言的函数 | `nil` |
| `WithValidation(tag, fn)` | 注册 tag 级别自定义校验 | — |
| `WithStructValidation(fn, types...)` | 注册 struct 级别跨字段校验 | — |

## Locale 解析优先级

```
localeExtractor(ctx) → defaultLocale
```

`localeExtractor` 返回 `false` 或返回的 locale 不在已注册列表中时，自动回退到 `defaultLocale`。
