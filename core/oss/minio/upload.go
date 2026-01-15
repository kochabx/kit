package minio

import (
	"context"
	"time"

	"github.com/minio/minio-go/v7"
)

// GetPresignedUploadURL 获取预签名上传URL
func (c *Client) GetPresignedUploadURL(ctx context.Context, params *PresignedUploadParams) (*PresignedUploadResult, error) {
	// 验证参数
	if err := params.Validate(); err != nil {
		return nil, err
	}

	// 检查对象是否已存在
	exists, err := c.ObjectExists(ctx, params.Bucket, params.Object)
	if err != nil {
		return nil, &ObjectError{
			Bucket:    params.Bucket,
			Object:    params.Object,
			Operation: "check_exists",
			Err:       err,
		}
	}

	// 如果对象已存在，返回标记
	if exists {
		return &PresignedUploadResult{
			AlreadyExists: true,
		}, nil
	}

	// 使用配置的默认过期时间（如果未指定）
	expires := params.Expires
	if expires == 0 {
		expires = c.config.PresignExpiry
	}

	// 生成预签名URL
	presignURL, err := c.core.PresignedPutObject(ctx, params.Bucket, params.Object, expires)
	if err != nil {
		return nil, &ObjectError{
			Bucket:    params.Bucket,
			Object:    params.Object,
			Operation: "presign_put",
			Err:       err,
		}
	}

	return &PresignedUploadResult{
		URL:       presignURL,
		ExpiresAt: time.Now().Add(expires),
	}, nil
}

// ObjectExists 检查对象是否存在
func (c *Client) ObjectExists(ctx context.Context, bucket, object string) (bool, error) {
	if bucket == "" {
		return false, ErrEmptyBucketName
	}
	if object == "" {
		return false, ErrEmptyObjectName
	}

	_, err := c.core.StatObject(ctx, bucket, object, minio.StatObjectOptions{})
	if err != nil {
		// 检查是否为对象不存在的错误
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || errResp.Code == "NotFound" {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
