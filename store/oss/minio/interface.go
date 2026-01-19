package minio

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
)

// StorageClient 对象存储客户端接口
type StorageClient interface {
	// 桶操作
	BucketOperations
	// 对象上传操作
	UploadOperations
	// 分片上传操作
	MultipartOperations
	// 生命周期管理
	io.Closer
}

// BucketOperations 桶操作接口
type BucketOperations interface {
	// CreateBucket 创建存储桶
	CreateBucket(ctx context.Context, bucketName string) error
	// DeleteBucket 删除存储桶
	DeleteBucket(ctx context.Context, bucketName string) error
	// BucketExists 检查存储桶是否存在
	BucketExists(ctx context.Context, bucketName string) (bool, error)
}

// UploadOperations 上传操作接口
type UploadOperations interface {
	// GetPresignedUploadURL 获取预签名上传URL
	GetPresignedUploadURL(ctx context.Context, params *PresignedUploadParams) (*PresignedUploadResult, error)
	// ObjectExists 检查对象是否存在
	ObjectExists(ctx context.Context, bucket, object string) (bool, error)
}

// MultipartOperations 分片上传操作接口
type MultipartOperations interface {
	// InitiateMultipartUpload 初始化分片上传
	InitiateMultipartUpload(ctx context.Context, params *InitiateMultipartParams) (*InitiateMultipartResult, error)
	// CompleteMultipartUpload 完成分片上传
	CompleteMultipartUpload(ctx context.Context, params *CompleteMultipartParams) (*minio.UploadInfo, error)
	// AbortMultipartUpload 取消分片上传
	AbortMultipartUpload(ctx context.Context, params *AbortMultipartParams) error
	// ListIncompleteParts 列出未完成的分片
	ListIncompleteParts(ctx context.Context, params *ListIncompletePartsParams) (*ListIncompletePartsResult, error)
}
