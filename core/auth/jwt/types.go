package jwt

// TokenPair Access Token 和 Refresh Token 对
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

// GenerateOptions Token 生成选项
type GenerateOptions struct {
	DeviceID string
}

// GenerateOption Token 生成选项函数
type GenerateOption func(*GenerateOptions)

// WithDeviceID 设置设备 ID
func WithDeviceID(deviceID string) GenerateOption {
	return func(o *GenerateOptions) {
		o.DeviceID = deviceID
	}
}
