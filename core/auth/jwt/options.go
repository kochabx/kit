package jwt

// Option 配置选项
type Option func(*Config)

// WithSecret 设置密钥
func WithSecret(secret string) Option {
	return func(c *Config) {
		c.Secret = secret
	}
}

// WithAccessTokenTTL 设置 Access Token TTL（秒）
func WithAccessTokenTTL(seconds int64) Option {
	return func(c *Config) {
		c.AccessTokenTTL = seconds
	}
}

// WithRefreshTokenTTL 设置 Refresh Token TTL（秒）
func WithRefreshTokenTTL(seconds int64) Option {
	return func(c *Config) {
		c.RefreshTokenTTL = seconds
	}
}

// WithSigningMethod 设置签名方法
func WithSigningMethod(method string) Option {
	return func(c *Config) {
		c.SigningMethod = method
	}
}

// WithIssuer 设置签发者
func WithIssuer(issuer string) Option {
	return func(c *Config) {
		c.Issuer = issuer
	}
}

// WithAudience 设置受众
func WithAudience(audience ...string) Option {
	return func(c *Config) {
		c.Audience = audience
	}
}

// CacheConfig 缓存配置
type CacheConfig struct {
	MultiLogin bool // 是否启用多点登录
	MaxDevices int  // 最大设备数量，0 表示无限制
}

// CacheOption 缓存选项
type CacheOption func(*CacheConfig)

// WithMultiLogin 启用多点登录
// maxDevices: 最大设备数量，0 表示无限制（默认）
func WithMultiLogin(maxDevices int) CacheOption {
	return func(c *CacheConfig) {
		c.MultiLogin = true
		c.MaxDevices = max(maxDevices, 0)
	}
}

// WithMaxDevices 设置最大设备数量
// maxDevices: 最大设备数量，0 表示无限制
func WithMaxDevices(maxDevices int) CacheOption {
	return func(c *CacheConfig) {
		c.MaxDevices = max(maxDevices, 0)
	}
}
