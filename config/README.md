# Config Module

基于 Viper 的配置管理模块，支持配置加载、环境变量覆盖、热加载和验证。

## 特性

- ✅ 自动环境变量覆盖
- ✅ 支持 YAML/JSON/TOML 等格式
- ✅ 配置热加载
- ✅ 配置验证
- ✅ 并发安全

## 快速开始

```go
package main

import (
    "log"
    "github.com/kochabx/kit/config"
)

type AppConfig struct {
    Server struct {
        Host string `json:"host" default:"localhost"`
        Port int    `json:"port" default:"8080"`
    } `json:"server"`
}

func main() {
    cfg := &AppConfig{}
    c := config.New(cfg)

    if err := c.Load(); err != nil {
        log.Fatal(err)
    }

    log.Printf("Server: %s:%d", cfg.Server.Host, cfg.Server.Port)
}
```

## 环境变量

环境变量自动覆盖配置文件，命名规则：`.` 转 `_`，全大写

| 配置路径 | 环境变量 |
|---------|---------|
| `server.host` | `SERVER_HOST` |
| `server.port` | `SERVER_PORT` |
| `database.max_connections` | `DATABASE_MAX_CONNECTIONS` |

**示例：**
```bash
# config.yaml
server:
  host: "localhost"
  port: 8080

# 环境变量覆盖
export SERVER_HOST="0.0.0.0"
export SERVER_PORT="9000"
```

**优先级：** 环境变量 > 配置文件 > 默认值

## 配置热加载

```go
c := config.New(cfg)
c.Load()

// 启动热加载
c.Watch()

// 手动重载
c.Reload()
```

## 配置验证

```go
type Config struct {
    Host   string `json:"host" validate:"required,hostname"`
    Port   int    `json:"port" validate:"min=1,max=65535"`
    APIKey string `json:"api_key" validate:"required,min=32"`
    Email  string `json:"email" validate:"omitempty,email"`
}
```

## 结构体标签

```go
type Config struct {
    Host string `json:"host" yaml:"host" default:"localhost" validate:"required"`
    Port int    `json:"port" yaml:"port" default:"8080" validate:"min=1,max=65535"`
}
```

- `json`/`yaml` - 字段名
- `default` - 默认值  
- `validate` - 验证规则

## 自定义 Loader

```go
import (
    "github.com/kochabx/kit/config"
    "github.com/kochabx/kit/core/validator"
    "github.com/spf13/viper"
)

loader := config.NewFileLoader(
    "app.yaml",
    []string{"/etc/myapp", "."},
    viper.New(),
    validator.Validate,
)
c := config.New(cfg, config.WithLoader(loader))
```

## 配置文件示例

**YAML:**
```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  host: "localhost"
  user: "postgres"
  password: ""  # 由环境变量提供
```

**JSON:**
```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  }
}
```

## 完整示例

```go
package main

import (
    "fmt"
    "log"
    "github.com/kochabx/kit/config"
)

type AppConfig struct {
    Server struct {
        Host string `json:"host" default:"0.0.0.0" validate:"required"`
        Port int    `json:"port" default:"8080" validate:"min=1,max=65535"`
    } `json:"server"`
    
    Database struct {
        Host     string `json:"host" default:"localhost" validate:"required"`
        User     string `json:"user" default:"postgres" validate:"required"`
        Password string `json:"password" validate:"required"`
    } `json:"database"`
}

func main() {
    cfg := &AppConfig{}
    c := config.New(cfg)
    
    if err := c.Load(); err != nil {
        log.Fatal(err)
    }
    
    if err := c.Watch(); err != nil {
        log.Warn(err)
    }
    
    fmt.Printf("Server: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
    
    select {}
}
```

**运行：**
```bash
export DATABASE_PASSWORD="secret"
export SERVER_PORT="80"
./myapp
```

## API 参考

### Config 方法

| 方法 | 说明 |
|-----|------|
| `New(target any, opts ...Option)` | 创建配置管理器 |
| `Load() error` | 加载配置 |
| `Reload() error` | 重新加载 |
| `Watch() error` | 启动监听 |
| `GetViper()` | 获取 viper 实例 |

### 选项

| 选项 | 说明 |
|-----|------|
| `WithLoader(loader)` | 自定义加载器 |

### FileLoader

```go
NewFileLoader(
    filename string,              // 配置文件名
    paths []string,               // 搜索路径
    viper *viper.Viper,
    validate validator.Validator,
)
```

## 最佳实践

### 1. 敏感信息
```yaml
# config.yaml
database:
  user: "postgres"
  # password 留空
```
```bash
export DATABASE_PASSWORD="secret"
```

### 2. 环境隔离
```go
env := os.Getenv("APP_ENV")
if env == "" {
    env = "dev"
}
filename := fmt.Sprintf("config.%s.yaml", env)
loader := config.NewFileLoader(filename, []string{"."}, viper.New(), validator.Validate)
```

### 3. 多路径搜索
```go
loader := config.NewFileLoader(
    "config.yaml",
    []string{"/etc/myapp", "$HOME/.myapp", "."},
    viper.New(),
    validator.Validate,
)
```

### 4. 错误处理
```go
if err := c.Load(); err != nil {
    if os.IsNotExist(err) {
        log.Warn("Config not found, using defaults")
        return nil
    }
    log.Fatal(err)
}
```

## 常见问题

**Q: 环境变量没生效？**  
A: 确保命名正确：`server.port` → `SERVER_PORT`

**Q: 配置文件找不到？**  
A: 检查文件名和搜索路径（默认：`config.yaml` 和 `["."]`）

**Q: 如何禁用热加载？**  
A: 不调用 `Watch()` 方法

**Q: 支持哪些格式？**  
A: YAML、JSON、TOML、Properties（根据扩展名识别）

## 注意事项

- 配置结构体字段必须可导出（首字母大写）
- 环境变量优先级最高
- `Watch()` 需在 `Load()` 之后调用
- 配置更新直接修改 target 结构体

## 项目文件

| 文件 | 说明 |
|-----|------|
| [config.go](config.go) | 配置管理器 |
| [loader.go](loader.go) | Loader 接口 |
| [file_loader.go](file_loader.go) | 文件加载器 |
| [option.go](option.go) | 选项模式 |
| [config_test.go](config_test.go) | 单元测试 |
