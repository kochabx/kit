package minio

import (
	"fmt"
	"mime"
	"path/filepath"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client MinIO 客户端
type Client struct {
	config *Config
	core   *minio.Core
}

// NewClient 创建新的 MinIO 客户端
func NewClient(endpoint, accessKeyID, secretAccessKey string, opts ...Option) (*Client, error) {
	// 使用默认配置
	config := DefaultConfig()
	config.Endpoint = endpoint
	config.AccessKeyID = accessKeyID
	config.SecretAccessKey = secretAccessKey

	// 应用选项
	for _, opt := range opts {
		opt(config)
	}

	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 创建 MinIO Core 客户端
	minioOpts := &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
		Region: config.Region,
	}

	// 使用自定义 HTTP 客户端（如果提供）
	if config.HTTPClient != nil {
		minioOpts.Transport = config.HTTPClient.Transport
	}

	core, err := minio.NewCore(config.Endpoint, minioOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio core: %w", err)
	}

	return &Client{
		config: config,
		core:   core,
	}, nil
}

// Close 关闭客户端（预留接口，当前无需特殊清理）
func (c *Client) Close() error {
	// 当前 minio.Core 没有需要清理的资源
	// 预留此方法以便未来扩展
	return nil
}

// GetConfig 获取客户端配置（只读）
func (c *Client) GetConfig() Config {
	return *c.config
}

// contentType 根据文件扩展名推断 Content-Type
func (c *Client) contentType(objectName string) string {
	ext := filepath.Ext(objectName)
	if contentType := mime.TypeByExtension(ext); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

// 确保 Client 实现了 StorageClient 接口
var _ StorageClient = (*Client)(nil)
