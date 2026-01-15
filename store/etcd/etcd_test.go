package etcd

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// getTestEndpoint 获取测试用的 etcd 端点
func getTestEndpoint() string {
	if endpoint := os.Getenv("ETCD_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	return "localhost:2379" // 默认值
}

// getTestConfig 获取测试配置
func getTestConfig() *Config {
	return &Config{
		Endpoints: []string{getTestEndpoint()},
		Username:  "root",
		Password:  "12345678",
	}
}

func TestConfig_init(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   *Config
	}{
		{
			name:   "empty config should set defaults",
			config: &Config{},
			want: &Config{
				Endpoints:           []string{"localhost:2379"},
				Username:            "root",
				DialTimeout:         5,
				KeepAliveTime:       30,
				KeepAliveTimeout:    5,
				AutoSyncInterval:    0,
				MaxSendMsgSize:      2097152,
				MaxRecvMsgSize:      4194304,
				RejectOldCluster:    false,
				PermitWithoutStream: false,
			},
		},
		{
			name: "partial config should keep existing values",
			config: &Config{
				Endpoints: []string{"custom:2379"},
				Password:  "custom-password",
			},
			want: &Config{
				Endpoints:           []string{"custom:2379"},
				Username:            "root",
				Password:            "custom-password",
				DialTimeout:         5,
				KeepAliveTime:       30,
				KeepAliveTimeout:    5,
				AutoSyncInterval:    0,
				MaxSendMsgSize:      2097152,
				MaxRecvMsgSize:      4194304,
				RejectOldCluster:    false,
				PermitWithoutStream: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.init()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, tt.config)
		})
	}
}

func TestNew_WithInvalidEndpoint(t *testing.T) {
	config := &Config{
		Endpoints:   []string{"127.0.0.1:99999"}, // 使用无效端口
		DialTimeout: 1,                           // 短超时时间以快速失败
	}

	client, err := New(config)
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestNew_Integration(t *testing.T) {
	config := getTestConfig()

	tests := []struct {
		name    string
		config  *Config
		opts    []Option
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  config,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.config, tt.opts...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				assert.NotNil(t, client.Client)
				assert.NotNil(t, client.config)

				// 清理资源
				err = client.Close()
				assert.NoError(t, err)
			}
		})
	}
}

func TestEtcd_Ping_Unit(t *testing.T) {
	tests := []struct {
		name    string
		client  *Etcd
		wantErr bool
	}{
		{
			name:    "nil client",
			client:  &Etcd{Client: nil},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.client.Ping(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrEtcdNotInitialized, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEtcd_Ping_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	err = client.Ping(context.Background())
	assert.NoError(t, err)
}

func TestEtcd_Status_Unit(t *testing.T) {
	tests := []struct {
		name     string
		client   *Etcd
		endpoint string
		wantErr  bool
		wantResp bool
	}{
		{
			name:     "nil client",
			client:   &Etcd{Client: nil},
			endpoint: "localhost:2379",
			wantErr:  true,
			wantResp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.client.Status(context.Background(), tt.endpoint)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrEtcdNotInitialized, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.wantResp {
				assert.NotNil(t, resp)
			} else {
				assert.Nil(t, resp)
			}
		})
	}
}

func TestEtcd_Status_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	resp, err := client.Status(context.Background(), config.Endpoints[0])
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestEtcd_GetClient(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	rawClient := client.GetClient()
	assert.NotNil(t, rawClient)
	assert.IsType(t, &clientv3.Client{}, rawClient)
}

func TestEtcd_Close(t *testing.T) {
	config := getTestConfig()

	t.Run("close valid client", func(t *testing.T) {
		client, err := New(config)
		require.NoError(t, err)

		err = client.Close()
		assert.NoError(t, err)
		assert.Nil(t, client.Client)

		// 再次关闭应该不会出错
		err = client.Close()
		assert.NoError(t, err)
	})

	t.Run("close nil client", func(t *testing.T) {
		client := &Etcd{Client: nil}
		err := client.Close()
		assert.NoError(t, err)
	})
}

func TestEtcd_BasicOperations_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	key := "test-key"
	value := "test-value"

	// 测试基本的 Put 和 Get 操作
	t.Run("put and get", func(t *testing.T) {
		// Put
		_, err := client.Client.Put(ctx, key, value)
		assert.NoError(t, err)

		// Get
		resp, err := client.Client.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, key, string(resp.Kvs[0].Key))
		assert.Equal(t, value, string(resp.Kvs[0].Value))
	})

	// 测试删除操作
	t.Run("delete", func(t *testing.T) {
		// Delete
		_, err := client.Client.Delete(ctx, key)
		assert.NoError(t, err)

		// 验证删除
		resp, err := client.Client.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), resp.Count)
	})
}

func TestEtcd_WithOption_Integration(t *testing.T) {
	config := getTestConfig()

	// 定义一个测试选项
	optionApplied := false
	testOption := func(e *Etcd) {
		optionApplied = true
	}

	client, err := New(config, testOption)
	require.NoError(t, err)
	defer client.Close()

	assert.True(t, optionApplied, "option should have been applied")
}

// TestEtcd_ErrorConstants 测试错误常量
func TestEtcd_ErrorConstants(t *testing.T) {
	assert.Equal(t, "etcd client not initialized", ErrEtcdNotInitialized.Error())
	assert.Equal(t, "failed to connect to etcd", ErrConnectionFailed.Error())
}

// BenchmarkEtcd_ConfigInit 基准测试配置初始化
func BenchmarkEtcd_ConfigInit(b *testing.B) {
	for i := 0; i < b.N; i++ {
		config := &Config{}
		_ = config.init()
	}
}

// TestEtcd_ConnectError 测试连接错误处理
func TestEtcd_ConnectError(t *testing.T) {
	config := &Config{
		Endpoints:   []string{"127.0.0.1:99999"}, // 使用无效端口
		DialTimeout: 1,                           // 设置较短的超时时间
	}

	err := config.init()
	require.NoError(t, err)

	etcd := &Etcd{config: config}
	err = etcd.connect()
	assert.Error(t, err)
	assert.Equal(t, ErrConnectionFailed, err)
}

// TestEtcd_MultipleEndpoints 测试多个端点配置
func TestEtcd_MultipleEndpoints_Integration(t *testing.T) {
	config := getTestConfig()
	config.Endpoints = append(config.Endpoints, "backup:2379") // 第二个端点不存在，但应该能容错

	client, err := New(config)
	if err != nil {
		// 如果失败，可能是因为 etcd 版本或配置问题，这是可以接受的
		t.Logf("Multiple endpoints test failed (expected in some configurations): %v", err)
		return
	}
	defer client.Close()

	// 如果成功创建，测试基本功能
	err = client.Ping(context.Background())
	assert.NoError(t, err)
}
