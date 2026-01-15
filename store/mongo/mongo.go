package mongo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var (
	ErrConnectionFailed = errors.New("failed to connect to mongodb")
)

// Mongo MongoDB 客户端包装器
type Mongo struct {
	Client *mongo.Client
	config *Config
}

// Option Mongo 配置选项函数类型
type Option func(*Mongo)

// New 创建新的 Mongo 实例
func New(config *Config, opts ...Option) (*Mongo, error) {
	m := &Mongo{
		config: config,
	}

	// 初始化配置
	if err := m.config.init(); err != nil {
		return nil, err
	}

	// 应用选项
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}

	// 创建客户端连接
	if err := m.connect(); err != nil {
		return nil, err
	}

	// 测试连接
	if err := m.Ping(context.TODO()); err != nil {
		// 如果连接失败，确保清理资源
		_ = m.Close()
		return nil, err
	}

	return m, nil
}

// connect 创建 MongoDB 连接
func (m *Mongo) connect() error {
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
		SetConnectTimeout(time.Duration(m.config.Timeout) * time.Second).
		SetServerSelectionTimeout(time.Duration(m.config.Timeout) * time.Second)

	client, err := mongo.Connect(context.TODO(), opts)
	if err != nil {
		return ErrConnectionFailed
	}

	m.Client = client
	return nil
}

// Ping 测试 MongoDB 连接是否正常
func (m *Mongo) Ping(ctx context.Context) error {
	return m.Client.Ping(ctx, readpref.Primary())
}

func (m *Mongo) Close() error {
	if m.Client == nil {
		return nil
	}

	if err := m.Client.Disconnect(context.TODO()); err != nil {
		return err
	}

	m.Client = nil // 清空引用，避免重复关闭
	return nil
}

// GetClient 获取 MongoDB 客户端
func (m *Mongo) GetClient() *mongo.Client {
	return m.Client
}

// Database 获取指定名称的数据库
func (m *Mongo) Database(name string) *mongo.Database {
	if m.Client == nil {
		return nil
	}
	return m.Client.Database(name)
}
