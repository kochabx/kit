# x/validate

基于 `github.com/go-playground/validator/v10` 的结构化校验器，支持实例作用域配置、中英文（`en` / `zh`）i18n 翻译，并统一映射为不含原始敏感值的 `*fault.Error`。

## 快速开始

```go
import "github.com/kochabx/kit/x/validate"

type CreateUserReq struct {
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=18,lte=120"`
}

v, err := validate.New(
	validate.WithDefaultLocale(validate.LocaleZH),
)
if err != nil {
	return err
}

if err := v.Struct(ctx, &req); err != nil {
	// 返回 *fault.Error（CodeInvalidArgument），可通过 Violations 提取字段明细
	violations, ok := validate.Violations(err)
	_, _ = violations, ok
	return err
}
```

默认启用 `WithRequiredStructEnabled()`，并使用 `"json"` tag 作为错误字段名。

## 选项与扩展

| 选项 | 说明 |
|---|---|
| `WithDefaultLocale(Locale)` | 默认语言（`LocaleEN` / `LocaleZH`，默认 `LocaleEN`） |
| `WithLocale(locale, entry)` | 注册或覆盖指定语言的翻译器 |
| `WithLocaleExtractor(fn)` | 从 `context.Context` 动态解析请求语言 |
| `WithFieldNameTag(tag)` | 字段名来源 tag（默认 `"json"`） |
| `WithValidation(tag, fn, callEvenIfNull...)` | 注册自定义字段校验规则 |
| `WithValidationCtx(tag, fn, callEvenIfNull...)` | 注册带 `Context` 的字段校验规则 |
| `WithStructValidation(fn, types...)` | 注册跨字段结构级校验规则 |
| `WithStructValidationCtx(fn, types...)` | 注册带 `Context` 的结构级校验规则 |

## API

- `(*Validator).Struct(ctx, s)`：校验结构体；
- `(*Validator).Var(ctx, field, tag)`：校验单个值；
- `(*Validator).Translator(locale)`：获取对应语言的 `ut.Translator`；
- `Violations(err)`：从错误链中提取 `[]Violation`（包含 `Field`、`Tag`、`Param`、`Path`、`Message`）。

## 验证

```bash
go test -race -count=1 ./x/validate/...
```
