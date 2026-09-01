# Config

基于 Viper 的类型化配置加载，支持默认值、校验、环境变量和热更新。

## 使用

默认读取工作目录中的 `config.yaml`，将配置键中的 `.` 映射为环境变量的 `_`，
并展开文件内的 `${NAME}` 占位符。

```go
type AppConfig struct {
	Server struct {
		Host string `mapstructure:"host" default:"0.0.0.0" validate:"required"`
		Port int    `mapstructure:"port" default:"8080" validate:"gte=1,lte=65535"`
	} `mapstructure:"server"`
}

loader, err := config.New[AppConfig]()
if err != nil {
	return err
}

cfg, err := loader.Load(ctx)
if err != nil {
	return err
}
```

需要自定义文件、环境变量、flags、alias 或预设值时，传入配置好的 Viper：

```go
v := viper.New()
v.SetConfigFile(configFile)
v.AutomaticEnv()

loader, err := config.New[AppConfig](config.WithViper(v))
```

调用 `Load` 或 `Watch` 后，不要再并发修改传入的 Viper。

## 配置来源

```go
config.WithSource(config.SourceFile)   // 默认：配置文件
config.WithSource(config.SourceRemote) // 远程配置，必须同时使用 WithRemote
config.WithSource(config.SourceValues) // Set、flags、环境变量和 defaults
```

也可以直接注册远程 Provider：

```go
loader, err := config.New[AppConfig](
	config.WithRemote("etcd3", endpoint, "/services/app/config", "yaml", 5*time.Second),
)
```

远程 Provider 的实现依赖由应用按 Viper 要求引入。

## 热更新

监听前必须成功调用 `Load`：

```go
err := loader.Watch(func(event config.Event[AppConfig]) {
	if event.Err != nil {
		logger.Error().Err(event.Err).Msg("reload configuration")
		return
	}
	applyReloadableSettings(event.Current)
})

// 远程配置
err = loader.WatchRemote(ctx, handler)
```

文件事件会经过 debounce。远程配置按 `WithRemote` 的 interval 轮询；正在进行的
Viper 远程读取不受 context 取消影响，因此 Provider 应配置请求超时。

无效配置不会替换当前快照，内容未变化时不会调用 handler。监听启动后再次调用
`Load` 会返回 `ErrAlreadyWatching`。

## 校验与快照

先执行 `validate` tag，再执行可选的跨字段校验：

```go
func (cfg *AppConfig) Validate(ctx context.Context) error {
	if cfg.Workers.Min > cfg.Workers.Max {
		return errors.New("minimum workers exceed maximum workers")
	}
	return nil
}
```

每次成功加载都会发布新的快照。`Load` 和 Watch handler 返回的配置必须视为只读，
包括其中的 map、slice 和 pointer。

错误可通过 `ErrRead`、`ErrDecode` 和 `ErrValidation` 判断类型。
