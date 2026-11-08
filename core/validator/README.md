# validator

基于 go-playground/validator 的校验封装：支持结构体、单值、多语言消息、嵌套字段路径和 Context 回调。构造完成后不再注册规则，可共享实例校验不同对象。

```go
type User struct {
    Name  string `json:"name" validate:"required"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"gte=18,lte=150"`
}

err := validator.Validate.Struct(ctx, &User{Email: "bad", Age: 10})
if ve, ok := errors.AsType[*validator.Error](err); ok {
    for _, violation := range ve.Violations() {
        fmt.Println(violation.Path, violation.Tag, violation.Message)
    }
}
```

`Struct` 支持结构体值和非 nil 结构体指针；无效输入返回底层 `*gv.InvalidValidationError`。`Var(ctx, value, "required,email")` 校验单个值。Context 必须非 nil。

## 配置

```go
v, err := validator.New(
    validator.WithDefaultLocale(validator.LocaleZH),
    validator.WithFieldNameTag("json"),
)
if err != nil {
    return err
}
err = v.Struct(ctx, &config)
```

| 选项 | 用途 |
|---|---|
| `WithDefaultLocale` | 默认语言，默认 en |
| `WithLocale` | 注册或覆盖语言，默认启用 en、zh |
| `WithFieldNameTag` | 错误字段名来源，默认 json；空字符串使用 Go 名称 |
| `WithLocaleExtractor` | 从 Context 获取语言；未命中或不支持时回退默认语言 |
| `WithValidation` | 注册字段校验 |
| `WithValidationCtx` | 注册可读取 Context 的字段校验 |
| `WithStructValidation` | 注册结构体级校验 |
| `WithStructValidationCtx` | 注册可读取 Context 的结构体级校验 |

`New` 检查 nil Option、语言配置、重复自定义标签、回调和结构体类型。构造期间注册或配置回调发生 panic 时转换为构造错误；可通过 `errors.Is(err, validator.ErrInvalidOption)` 判断。默认 `Validate` 初始化失败会立即 panic，不会静默留下 nil 实例。

`WithLocale` 使用 `LocaleEntry{Loc: ..., Register: ...}`；两项必须完整，不同语言键不能指向同名 Translator。

## Context 校验

```go
v, err := validator.New(
    validator.WithValidationCtx("allowed", func(ctx context.Context, fl gv.FieldLevel) bool {
        return policy.Allowed(ctx, fl.Field().String())
    }),
    validator.WithStructValidationCtx(func(ctx context.Context, sl gv.StructLevel) {
        // 使用 ctx 和 sl 检查跨字段规则，失败时调用 sl.ReportError。
    }, User{}),
)
```

示例中的 `gv` 指 `github.com/go-playground/validator/v10`。Context 回调自行处理取消和超时；传递 Context 不代表校验器会自动中断执行。

字段校验的 `callEvenIfNull` 最多传一个 bool，用于控制是否对 nil 值执行回调；普通零值是否跳过仍由 `omitempty` 等标签决定。

## 错误模型

```go
type Violation struct {
    Field   string // 简短字段名，例如 email
    Path    string // 完整路径，例如 Request.users[0].email
    Tag     string
    Param   string
    Message string
}
```

- 字段校验失败返回 `*validator.Error`；`validator.AsError(err)` 用于判断错误链中是否存在该类型。
- `Violations()` 返回切片副本，修改它不会改变错误对象；单值校验的字段名和路径可能为空。
- `Error.Unwrap()` 保留原始错误，可通过 `errors.AsType[gv.ValidationErrors](err)` 获取原始字段值。这是显式低层访问，不应直接将原始错误对象作为响应输出。
- 已移除 `Violation.Value`，默认结构化错误不保存输入值。自定义翻译消息和约束参数仍由调用方负责避免包含敏感信息。
- 非法或未注册的运行时校验标签属于编程错误，保留底层 panic 行为；不会伪装成用户输入校验失败。

## 并发与集成

实例内部在 `New` 完成后只读。自定义校验、语言提取和翻译回调必须自行保证并发安全；调用方不得同时修改被校验数据。自定义语言注册函数应只在构造时注册，不要保留 Validator/Translator 后在运行中修改。

配置加载顺序建议为：读取配置 → `defaults.Apply` → `Validator.Struct`。
HTTP 中间件可将语言写入 Context，再由 `WithLocaleExtractor` 读取。使用 Gin 等框架时，应明确选择框架自动校验还是本模块校验，避免重复执行；本模块不会修改全局框架配置。

## 验证

```bash
go test -race ./core/validator -count=1
```

测试覆盖多语言回退、非法构造参数、字段路径、错误副本、Context 回调及并发校验；不需要外部服务。
