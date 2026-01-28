package mongo

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/kochabx/kit/log"
)

// Client MongoDB 客户端包装器
type Client struct {
	client *mongo.Client
	config *Config
	logger *log.Logger
}

// New 创建新的 Mongo 客户端
func New(config *Config, opts ...Option) (*Client, error) {
	if config == nil {
		return nil, errors.New("config is required")
	}

	// 初始化配置
	if err := config.Init(); err != nil {
		return nil, err
	}

	options := &clientOptions{
		logger: log.G,
	}

	for _, opt := range opts {
		opt(options)
	}

	m := &Client{
		config: config,
		logger: options.logger,
	}

	// 创建客户端连接
	if err := m.connect(); err != nil {
		return nil, err
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	if err := m.Ping(ctx); err != nil {
		_ = m.Close()
		return nil, err
	}

	return m, nil
}

// connect 创建 MongoDB 连接
func (m *Client) connect() error {
	serverApi := options.ServerAPI(options.ServerAPIVersion1)
	bsonOpts := &options.BSONOptions{
		UseJSONStructTags: true,
		NilSliceAsEmpty:   true,
	}

	opts := options.Client().
		ApplyURI(m.config.uri()).
		SetServerAPIOptions(serverApi).
		SetBSONOptions(bsonOpts).
		SetMaxPoolSize(uint64(m.config.MaxPoolSize)).
		SetConnectTimeout(m.config.Timeout).
		SetServerSelectionTimeout(m.config.Timeout)

	client, err := mongo.Connect(context.TODO(), opts)
	if err != nil {
		return ErrConnectionFailed
	}

	m.client = client
	return nil
}

// Ping 测试 MongoDB 连接是否正常
func (m *Client) Ping(ctx context.Context) error {
	return m.client.Ping(ctx, readpref.Primary())
}

// Close 关闭客户端
func (m *Client) Close() error {
	if m.client == nil {
		return nil
	}

	if err := m.client.Disconnect(context.TODO()); err != nil {
		return err
	}

	return nil
}

// GetClient 获取 MongoDB 客户端
func (m *Client) GetClient() *mongo.Client {
	return m.client
}

// Database 获取指定名称的数据库
func (m *Client) Database(name string) *mongo.Database {
	if m.client == nil {
		return nil
	}
	return m.client.Database(name)
}
