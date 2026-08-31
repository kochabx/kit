# Config

`config` 在 Viper 之上提供类型化加载、默认值、校验和不可变配置快照。

## 默认用法

默认读取工作目录中的 `config.yaml`，开启环境变量覆盖，并支持文件中的
`${NAME}` 环境变量占位符。

```go
cfg, err := config.New[AppConfig]()
if err != nil {
	return err
}

current, err := cfg.Load(ctx)
if err != nil {
	return err
}
```

环境变量名称默认把配置键中的 `.` 替换为 `_`。例如 `server.port` 对应
`SERVER_PORT`。

配置结构可以使用 Viper 默认支持的 `mapstructure` tag，以及项目提供的
`default` 和 `validate` tag：

```go
type AppConfig struct {
	Server struct {
		Host string `mapstructure:"host" default:"0.0.0.0" validate:"required"`
		Port int    `mapstructure:"port" default:"8080" validate:"gte=1,lte=65535"`
	} `mapstructure:"server"`
}
```

## 自定义 Viper

需要自定义文件路径、`BindPFlag`、alias、`Set` 或远程 Provider 时，先配置
Viper，再通过 `WithViper` 传入：

```go
v := viper.New()
v.SetConfigFile(configFile)
v.SetEnvPrefix("APP")
v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
v.AutomaticEnv()
_ = v.BindPFlag("server.port", flags.Lookup("port"))
v.RegisterAlias("http.port", "server.port")
v.Set("build.version", version)

cfg, err := config.New[AppConfig](
	config.WithViper(v),
)
```

config package 不重复封装 Viper 的这些 API。调用 `Load` 或 `Watch` 后，不应
再从其他 goroutine 并发修改传入的 Viper。

## 配置来源

未传 `WithSource` 时默认使用 `SourceFile`。

```go
const (
	SourceFile   // ReadInConfig
	SourceRemote // ReadRemoteConfig
	SourceValues // 只解析 Viper 当前已有的值
)
```

远程 Provider 示例：

```go
cfg, err := config.New[AppConfig](
	config.WithRemote(
		"etcd3",
		endpoint,
		"/services/app/config",
		"yaml",
		5*time.Second,
	),
)
```

远程 Provider 所需依赖由应用按 Viper 的要求引入。

## Watch

必须先成功调用 `Load`：

```go
err := cfg.Watch(func(event config.Event[AppConfig]) {
	if event.Err != nil {
		logger.Error().Err(event.Err).Msg("reload configuration")
		return
	}
	applyReloadableSettings(event.Current)
})
```

文件监听直接使用 Viper 的 `WatchConfig`，回调中仅做短暂等待，避免一次文件写入
产生的中间状态被发布。Viper 没有停止文件 watcher 的 API，因此文件监听通常
与进程同生命周期。远程配置使用可取消的独立方法：

```go
err := cfg.WatchRemote(ctx, handler)
```

它按 `WithRemote` 中指定的 interval 刷新，并在 context 取消时退出。

无效配置通过 `Event.Err` 报告，不会替换当前快照。内容没有变化时不会调用
handler。

## 快照与校验

每次成功加载都会创建并原子发布一个新的 `*AppConfig`。调用方持有 `Load`
返回的初始值，并通过 Watch handler 接收后续更新。所有快照都必须视为只读值。

首先执行 `validate` tag。配置还可以实现跨字段校验：

```go
func (cfg *AppConfig) Validate(ctx context.Context) error {
	if cfg.Workers.Min > cfg.Workers.Max {
		return errors.New("minimum workers exceed maximum workers")
	}
	return nil
}
```

所有读取、解析和校验错误都使用 `%w` 保留错误链，可通过 `ErrRead`、
`ErrDecode` 和 `ErrValidation` 判断类型。
