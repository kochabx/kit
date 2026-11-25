# Config

基于 **`github.com/spf13/viper`** 的文件配置加载器，支持环境变量覆盖、`${NAME}` 占位符展开、原子只读快照，以及可取消、无 goroutine 泄露的 `fsnotify` 热更新。

## 使用

默认读取工作目录中的 `config.yaml`，按 `json` tag 解码（可通过 `WithTag` 覆盖，无需额外写 `mapstructure` tag），将配置键中的 `.` 映射为环境变量的 `_`，展开配置值内的 `${NAME}` 占位符，并在解码前后自动应用 `default` tag（[`x/defaults`](../x/defaults/README.md)）与校验 `validate` tag（[`x/validate`](../x/validate/README.md)）及可选 `Validate(ctx)` 方法：

```go
type AppConfig struct {
	Server struct {
		Host string `json:"host" default:"0.0.0.0" validate:"required"`
		Port int    `json:"port" default:"8080" validate:"gte=1,lte=65535"`
	} `json:"server"`
}

loader, err := config.New[AppConfig](
	config.WithFile("config.yaml"),
	// 可选自定义验证器：
	// config.WithValidator(customValidator),
)
if err != nil {
	return err
}
defer loader.Close()

cfg, err := loader.Load(ctx)
if err != nil {
	return err
}
_ = loader.Current() // 无锁读取最新快照
```

需要自定义 flags（`pflag`）、alias、环境变量前缀或预设值时，可直接传入配置好的 `*viper.Viper`：

```go
v := viper.New()
v.SetConfigFile(configFile)
v.AutomaticEnv()

loader, err := config.New[AppConfig](config.WithViper(v))
```

> 调用 `Load` 或 `Watch` 后，请勿再并发修改传入的 `*viper.Viper` 实例。

## 热更新

监听前必须成功调用 `Load`：

```go
// 基于可关闭的 fsnotify，随 ctx 取消或 Close() 自动回收全部协程
err := loader.Watch(ctx, func(event config.Event[AppConfig]) {
	if event.Err != nil {
		slog.Error("reload configuration", slog.Any("err", event.Err))
		return
	}
	applyReloadableSettings(event.Current)
})
```

- 文件事件经过可配置 debounce（默认 `50ms`，可通过 `WithWatchDebounce` 调整）并自动处理符号链接切换；
- 无效配置不会覆盖当前快照（`Current()` 仍保持上一份有效配置），内容未变化时不触发事件；
- 返回的 `*T` 快照视为只读。
