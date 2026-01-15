package minio

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEndpoint  = "localhost:9000"
	testAccessKey = "minioadmin"
	testSecretKey = "minioadmin"
	testBucket    = "test-bucket"
)

// 测试环境设置辅助函数
func setupTestClient(t *testing.T) *Client {
	client, err := NewClient(
		testEndpoint,
		testAccessKey,
		testSecretKey,
		WithUseSSL(false),
		WithMaxConcurrency(5),
		WithRequestTimeout(10*time.Second),
	)
	require.NoError(t, err, "failed to create client")
	return client
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		accessKey   string
		secretKey   string
		opts        []Option
		expectError bool
	}{
		{
			name:        "valid config",
			endpoint:    testEndpoint,
			accessKey:   testAccessKey,
			secretKey:   testSecretKey,
			opts:        []Option{WithUseSSL(false)},
			expectError: false,
		},
		{
			name:        "empty endpoint",
			endpoint:    "",
			accessKey:   testAccessKey,
			secretKey:   testSecretKey,
			expectError: true,
		},
		{
			name:        "empty access key",
			endpoint:    testEndpoint,
			accessKey:   "",
			secretKey:   testSecretKey,
			expectError: true,
		},
		{
			name:        "empty secret key",
			endpoint:    testEndpoint,
			accessKey:   testAccessKey,
			secretKey:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.endpoint, tt.accessKey, tt.secretKey, tt.opts...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				if client != nil {
					assert.NoError(t, client.Close())
				}
			}
		})
	}
}

func TestBucketOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	client := setupTestClient(t)
	defer client.Close()

	ctx := context.Background()
	bucketName := "test-bucket-ops"

	// 清理：确保测试开始时桶不存在
	_ = client.DeleteBucket(ctx, bucketName)

	t.Run("create bucket", func(t *testing.T) {
		err := client.CreateBucket(ctx, bucketName)
		assert.NoError(t, err)

		// 验证桶已创建
		exists, err := client.BucketExists(ctx, bucketName)
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("create existing bucket", func(t *testing.T) {
		err := client.CreateBucket(ctx, bucketName)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrBucketExists)
	})

	t.Run("delete bucket", func(t *testing.T) {
		err := client.DeleteBucket(ctx, bucketName)
		assert.NoError(t, err)

		// 验证桶已删除
		exists, err := client.BucketExists(ctx, bucketName)
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("delete non-existent bucket", func(t *testing.T) {
		err := client.DeleteBucket(ctx, bucketName)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrBucketNotFound)
	})
}

func TestGetPresignedUploadURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	client := setupTestClient(t)
	defer client.Close()

	ctx := context.Background()

	// 确保测试桶存在
	_ = client.CreateBucket(ctx, testBucket)
	defer client.DeleteBucket(ctx, testBucket)

	t.Run("valid request", func(t *testing.T) {
		params := &PresignedUploadParams{
			Bucket:  testBucket,
			Object:  "test-object.txt",
			Expires: 1 * time.Hour,
		}

		result, err := client.GetPresignedUploadURL(ctx, params)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.URL)
		assert.False(t, result.AlreadyExists)
	})

	t.Run("empty bucket", func(t *testing.T) {
		params := &PresignedUploadParams{
			Bucket: "",
			Object: "test-object.txt",
		}

		result, err := client.GetPresignedUploadURL(ctx, params)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrEmptyBucketName)
	})

	t.Run("empty object", func(t *testing.T) {
		params := &PresignedUploadParams{
			Bucket: testBucket,
			Object: "",
		}

		result, err := client.GetPresignedUploadURL(ctx, params)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrEmptyObjectName)
	})
}

func TestMultipartUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	client := setupTestClient(t)
	defer client.Close()

	ctx := context.Background()

	// 确保测试桶存在
	_ = client.CreateBucket(ctx, testBucket)
	defer client.DeleteBucket(ctx, testBucket)

	objectName := "test-multipart-object.bin"

	t.Run("initiate multipart upload", func(t *testing.T) {
		params := &InitiateMultipartParams{
			Bucket:     testBucket,
			Object:     objectName,
			ObjectSize: 100 * 1024 * 1024, // 100MB
			PartSize:   10 * 1024 * 1024,  // 10MB
			Expires:    1 * time.Hour,
		}

		result, err := client.InitiateMultipartUpload(ctx, params)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotEmpty(t, result.UploadID)
		assert.Equal(t, 10, result.TotalParts)
		assert.Len(t, result.Parts, 10)

		// 验证每个分片的信息
		for i, part := range result.Parts {
			assert.Equal(t, i+1, part.PartNumber)
			assert.NotNil(t, part.Url)
		}

		// 清理：取消上传
		_ = client.AbortMultipartUpload(ctx, &AbortMultipartParams{
			Bucket:   testBucket,
			Object:   objectName,
			UploadID: result.UploadID,
		})
	})

	t.Run("invalid part size", func(t *testing.T) {
		params := &InitiateMultipartParams{
			Bucket:     testBucket,
			Object:     objectName,
			ObjectSize: 100 * 1024 * 1024,
			PartSize:   1024, // Too small
		}

		result, err := client.InitiateMultipartUpload(ctx, params)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidPartSize)
	})
}

func TestContentType(t *testing.T) {
	client := &Client{config: DefaultConfig()}

	tests := []struct {
		filename   string
		expectedCT string
	}{
		{"test.txt", "text/plain; charset=utf-8"},
		{"test.jpg", "image/jpeg"},
		{"test.png", "image/png"},
		{"test.pdf", "application/pdf"},
		{"test.unknown", "application/octet-stream"},
		{"noextension", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			ct := client.contentType(tt.filename)
			assert.Equal(t, tt.expectedCT, ct)
		})
	}
}
