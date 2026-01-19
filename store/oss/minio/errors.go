package minio

import (
	"errors"
	"fmt"
)

// 预定义错误
var (
	ErrBucketExists        = errors.New("bucket already exists")
	ErrBucketNotFound      = errors.New("bucket not found")
	ErrInvalidPartSize     = errors.New("invalid part size")
	ErrObjectAlreadyExists = errors.New("object already exists")
	ErrInvalidConfig       = errors.New("invalid configuration")
	ErrEmptyBucketName     = errors.New("bucket name cannot be empty")
	ErrEmptyObjectName     = errors.New("object name cannot be empty")
	ErrInvalidObjectSize   = errors.New("object size must be greater than 0")
	ErrUploadIDNotFound    = errors.New("upload ID not found")
	ErrContextCanceled     = errors.New("context canceled")
)

// BucketError 桶操作错误
type BucketError struct {
	Bucket    string
	Operation string
	Err       error
}

func (e *BucketError) Error() string {
	return fmt.Sprintf("bucket operation failed: op=%s, bucket=%s, error=%v",
		e.Operation, e.Bucket, e.Err)
}

func (e *BucketError) Unwrap() error {
	return e.Err
}

// ObjectError 对象操作错误
type ObjectError struct {
	Bucket    string
	Object    string
	Operation string
	Err       error
}

func (e *ObjectError) Error() string {
	return fmt.Sprintf("object operation failed: op=%s, bucket=%s, object=%s, error=%v",
		e.Operation, e.Bucket, e.Object, e.Err)
}

func (e *ObjectError) Unwrap() error {
	return e.Err
}

// MultipartError 分片上传错误
type MultipartError struct {
	Bucket     string
	Object     string
	UploadID   string
	PartNumber int
	Err        error
}

func (e *MultipartError) Error() string {
	return fmt.Sprintf("multipart upload failed: bucket=%s, object=%s, uploadID=%s, part=%d, error=%v",
		e.Bucket, e.Object, e.UploadID, e.PartNumber, e.Err)
}

func (e *MultipartError) Unwrap() error {
	return e.Err
}
