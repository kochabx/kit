package minio

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
)

// CreateBucket 创建存储桶
func (c *Client) CreateBucket(ctx context.Context, bucketName string) error {
	if bucketName == "" {
		return ErrEmptyBucketName
	}

	// 检查桶是否已存在
	exists, err := c.BucketExists(ctx, bucketName)
	if err != nil {
		return &BucketError{
			Bucket:    bucketName,
			Operation: "check_exists",
			Err:       err,
		}
	}

	if exists {
		return &BucketError{
			Bucket:    bucketName,
			Operation: "create",
			Err:       ErrBucketExists,
		}
	}

	// 创建桶
	opts := minio.MakeBucketOptions{
		Region: c.config.Region,
	}

	if err := c.core.MakeBucket(ctx, bucketName, opts); err != nil {
		return &BucketError{
			Bucket:    bucketName,
			Operation: "create",
			Err:       err,
		}
	}

	return nil
}

// DeleteBucket 删除存储桶
func (c *Client) DeleteBucket(ctx context.Context, bucketName string) error {
	if bucketName == "" {
		return ErrEmptyBucketName
	}

	// 检查桶是否存在
	exists, err := c.BucketExists(ctx, bucketName)
	if err != nil {
		return &BucketError{
			Bucket:    bucketName,
			Operation: "check_exists",
			Err:       err,
		}
	}

	if !exists {
		return &BucketError{
			Bucket:    bucketName,
			Operation: "delete",
			Err:       ErrBucketNotFound,
		}
	}

	// 删除桶
	if err := c.core.RemoveBucket(ctx, bucketName); err != nil {
		return &BucketError{
			Bucket:    bucketName,
			Operation: "delete",
			Err:       err,
		}
	}

	return nil
}

// BucketExists 检查存储桶是否存在
func (c *Client) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	if bucketName == "" {
		return false, ErrEmptyBucketName
	}

	exists, err := c.core.BucketExists(ctx, bucketName)
	if err != nil {
		return false, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	return exists, nil
}
