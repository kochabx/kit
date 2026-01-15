package minio

import (
	"net/url"
	"time"
)

// PresignedUploadParams 预签名上传参数
type PresignedUploadParams struct {
	Bucket  string        // 存储桶名称
	Object  string        // 对象名称
	Expires time.Duration // 过期时间（可选，使用配置默认值）
}

// Validate 验证参数
func (p *PresignedUploadParams) Validate() error {
	if p.Bucket == "" {
		return ErrEmptyBucketName
	}
	if p.Object == "" {
		return ErrEmptyObjectName
	}
	return nil
}

// PresignedUploadResult 预签名上传结果
type PresignedUploadResult struct {
	URL           *url.URL  // 预签名URL
	AlreadyExists bool      // 对象是否已存在
	ExpiresAt     time.Time // URL过期时间
}

// InitiateMultipartParams 初始化分片上传参数
type InitiateMultipartParams struct {
	Bucket     string        // 存储桶名称
	Object     string        // 对象名称
	ObjectSize uint64        // 对象总大小
	PartSize   uint64        // 分片大小（可选，使用配置默认值）
	Expires    time.Duration // 预签名URL过期时间（可选）
}

// Validate 验证参数
func (p *InitiateMultipartParams) Validate() error {
	if p.Bucket == "" {
		return ErrEmptyBucketName
	}
	if p.Object == "" {
		return ErrEmptyObjectName
	}
	if p.ObjectSize == 0 {
		return ErrInvalidObjectSize
	}
	return nil
}

// InitiateMultipartResult 初始化分片上传结果
type InitiateMultipartResult struct {
	UploadID      string     // 上传ID
	AlreadyExists bool       // 对象是否已存在
	Parts         []PartInfo // 分片信息列表
	TotalParts    int        // 总分片数
}

// PartInfo 分片信息
type PartInfo struct {
	PartNumber int      `json:"part_number"` // 分片编号（从1开始）
	BeginSize  uint64   `json:"begin_size"`  // 起始字节位置
	EndSize    uint64   `json:"end_size"`    // 结束字节位置（不包含）
	Url        *url.URL `json:"url"`         // 预签名上传URL
}

// CompleteMultipartParams 完成分片上传参数
type CompleteMultipartParams struct {
	Bucket   string // 存储桶名称
	Object   string // 对象名称
	UploadID string // 上传ID
}

// Validate 验证参数
func (p *CompleteMultipartParams) Validate() error {
	if p.Bucket == "" {
		return ErrEmptyBucketName
	}
	if p.Object == "" {
		return ErrEmptyObjectName
	}
	if p.UploadID == "" {
		return ErrUploadIDNotFound
	}
	return nil
}

// AbortMultipartParams 取消分片上传参数
type AbortMultipartParams struct {
	Bucket   string // 存储桶名称
	Object   string // 对象名称
	UploadID string // 上传ID
}

// Validate 验证参数
func (p *AbortMultipartParams) Validate() error {
	if p.Bucket == "" {
		return ErrEmptyBucketName
	}
	if p.Object == "" {
		return ErrEmptyObjectName
	}
	if p.UploadID == "" {
		return ErrUploadIDNotFound
	}
	return nil
}

// ListIncompletePartsParams 列出未完成分片参数
type ListIncompletePartsParams struct {
	Bucket string // 存储桶名称
	Object string // 对象名称
}

// Validate 验证参数
func (p *ListIncompletePartsParams) Validate() error {
	if p.Bucket == "" {
		return ErrEmptyBucketName
	}
	if p.Object == "" {
		return ErrEmptyObjectName
	}
	return nil
}

// ListIncompletePartsResult 未完成分片列表结果
type ListIncompletePartsResult struct {
	UploadID       string           // 上传ID
	CompletedParts map[int]struct{} // 已完成的分片编号集合
}
