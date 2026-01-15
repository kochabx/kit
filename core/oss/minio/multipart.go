package minio

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// InitiateMultipartUpload 初始化分片上传
func (c *Client) InitiateMultipartUpload(ctx context.Context, params *InitiateMultipartParams) (*InitiateMultipartResult, error) {
	// 验证参数
	if err := params.Validate(); err != nil {
		return nil, err
	}

	// 使用配置的默认分片大小（如果未指定）
	partSize := params.PartSize
	if partSize == 0 {
		partSize = c.config.PartSize
	}

	// 验证分片大小
	if partSize < c.config.MinPartSize || partSize > c.config.MaxPartSize {
		return nil, &MultipartError{
			Bucket: params.Bucket,
			Object: params.Object,
			Err:    fmt.Errorf("%w: must be between %d and %d bytes", ErrInvalidPartSize, c.config.MinPartSize, c.config.MaxPartSize),
		}
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

	if exists {
		return &InitiateMultipartResult{
			AlreadyExists: true,
		}, nil
	}

	// 检查是否有未完成的上传
	incompleteResult, err := c.ListIncompleteParts(ctx, &ListIncompletePartsParams{
		Bucket: params.Bucket,
		Object: params.Object,
	})
	if err != nil {
		return nil, err
	}

	uploadID := incompleteResult.UploadID

	// 如果没有未完成的上传，创建新的
	if uploadID == "" {
		uploadID, err = c.core.NewMultipartUpload(ctx, params.Bucket, params.Object, minio.PutObjectOptions{
			ContentType: c.contentType(params.Object),
		})
		if err != nil {
			return nil, &MultipartError{
				Bucket: params.Bucket,
				Object: params.Object,
				Err:    fmt.Errorf("failed to initiate multipart upload: %w", err),
			}
		}
	}

	// 计算分片信息
	parts := c.calculateParts(params.ObjectSize, partSize)

	// 使用配置的默认过期时间（如果未指定）
	expires := params.Expires
	if expires == 0 {
		expires = c.config.PresignExpiry
	}

	// 并发生成预签名URL
	result, err := c.generatePresignedPartURLs(ctx, params.Bucket, params.Object, uploadID, parts, incompleteResult.CompletedParts, expires)
	if err != nil {
		// 如果生成URL失败，尝试清理已创建的上传
		_ = c.AbortMultipartUpload(ctx, &AbortMultipartParams{
			Bucket:   params.Bucket,
			Object:   params.Object,
			UploadID: uploadID,
		})
		return nil, err
	}

	result.UploadID = uploadID
	result.TotalParts = len(parts)

	return result, nil
}

// CompleteMultipartUpload 完成分片上传
func (c *Client) CompleteMultipartUpload(ctx context.Context, params *CompleteMultipartParams) (*minio.UploadInfo, error) {
	// 验证参数
	if err := params.Validate(); err != nil {
		return nil, err
	}

	// 列出所有已上传的分片
	partsResult, err := c.core.ListObjectParts(ctx, params.Bucket, params.Object, params.UploadID, 0, c.config.MaxParts)
	if err != nil {
		return nil, &MultipartError{
			Bucket:   params.Bucket,
			Object:   params.Object,
			UploadID: params.UploadID,
			Err:      fmt.Errorf("failed to list object parts: %w", err),
		}
	}

	if len(partsResult.ObjectParts) == 0 {
		return &minio.UploadInfo{}, &MultipartError{
			Bucket:   params.Bucket,
			Object:   params.Object,
			UploadID: params.UploadID,
			Err:      fmt.Errorf("no parts uploaded"),
		}
	}

	// 构造完成请求的分片列表
	completeParts := make([]minio.CompletePart, len(partsResult.ObjectParts))
	for i, part := range partsResult.ObjectParts {
		completeParts[i] = minio.CompletePart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}

	// 按分片编号排序
	sort.Slice(completeParts, func(i, j int) bool {
		return completeParts[i].PartNumber < completeParts[j].PartNumber
	})

	// 完成分片上传
	uploadInfo, err := c.core.CompleteMultipartUpload(ctx, params.Bucket, params.Object, params.UploadID, completeParts, minio.PutObjectOptions{
		ContentType: c.contentType(params.Object),
	})
	if err != nil {
		return nil, &MultipartError{
			Bucket:   params.Bucket,
			Object:   params.Object,
			UploadID: params.UploadID,
			Err:      fmt.Errorf("failed to complete multipart upload: %w", err),
		}
	}

	return &uploadInfo, nil
}

// AbortMultipartUpload 取消分片上传
func (c *Client) AbortMultipartUpload(ctx context.Context, params *AbortMultipartParams) error {
	// 验证参数
	if err := params.Validate(); err != nil {
		return err
	}

	if err := c.core.AbortMultipartUpload(ctx, params.Bucket, params.Object, params.UploadID); err != nil {
		return &MultipartError{
			Bucket:   params.Bucket,
			Object:   params.Object,
			UploadID: params.UploadID,
			Err:      fmt.Errorf("failed to abort multipart upload: %w", err),
		}
	}

	return nil
}

// ListIncompleteParts 列出未完成的分片
func (c *Client) ListIncompleteParts(ctx context.Context, params *ListIncompletePartsParams) (*ListIncompletePartsResult, error) {
	// 验证参数
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result := &ListIncompletePartsResult{
		CompletedParts: make(map[int]struct{}),
	}

	// 列出未完成的上传
	uploadsCh := c.core.ListIncompleteUploads(ctx, params.Bucket, params.Object, true)

	for uploadInfo := range uploadsCh {
		if uploadInfo.Err != nil {
			return nil, &ObjectError{
				Bucket:    params.Bucket,
				Object:    params.Object,
				Operation: "list_incomplete_uploads",
				Err:       uploadInfo.Err,
			}
		}

		// 只处理匹配的对象
		if uploadInfo.Key == params.Object {
			result.UploadID = uploadInfo.UploadID

			// 列出已完成的分片
			parts, err := c.core.ListObjectParts(ctx, params.Bucket, params.Object, uploadInfo.UploadID, 0, c.config.MaxParts)
			if err != nil {
				return nil, &MultipartError{
					Bucket:   params.Bucket,
					Object:   params.Object,
					UploadID: uploadInfo.UploadID,
					Err:      fmt.Errorf("failed to list object parts: %w", err),
				}
			}

			// 记录已完成的分片
			for _, part := range parts.ObjectParts {
				result.CompletedParts[part.PartNumber] = struct{}{}
			}

			// 找到第一个匹配的就退出
			break
		}
	}

	return result, nil
}

// calculateParts 计算分片信息
func (c *Client) calculateParts(objectSize, partSize uint64) []PartInfo {
	partCount := int(math.Ceil(float64(objectSize) / float64(partSize)))
	parts := make([]PartInfo, 0, partCount)

	for i := 0; i < partCount; i++ {
		startByte := uint64(i) * partSize
		endByte := startByte + partSize
		if endByte > objectSize {
			endByte = objectSize
		}

		parts = append(parts, PartInfo{
			PartNumber: i + 1,
			BeginSize:  startByte,
			EndSize:    endByte,
		})
	}

	return parts
}

// generatePresignedPartURLs 并发生成分片的预签名URL
func (c *Client) generatePresignedPartURLs(
	ctx context.Context,
	bucket, object, uploadID string,
	parts []PartInfo,
	completedParts map[int]struct{},
	expires time.Duration,
) (*InitiateMultipartResult, error) {
	result := &InitiateMultipartResult{
		Parts: make([]PartInfo, 0, len(parts)),
	}

	// 使用 semaphore 控制并发
	sem := semaphore.NewWeighted(int64(c.config.MaxConcurrency))
	eg, egCtx := errgroup.WithContext(ctx)

	// 用于收集结果的 channel
	resultCh := make(chan PartInfo, len(parts))

	// 并发生成预签名URL
	for _, part := range parts {
		// 跳过已完成的分片
		if _, completed := completedParts[part.PartNumber]; completed {
			resultCh <- part
			continue
		}

		part := part // 捕获循环变量

		eg.Go(func() error {
			// 获取信号量
			if err := sem.Acquire(egCtx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			// 检查上下文是否已取消
			select {
			case <-egCtx.Done():
				return egCtx.Err()
			default:
			}

			// 构造查询参数
			urlValues := url.Values{}
			urlValues.Set("uploadId", uploadID)
			urlValues.Set("partNumber", strconv.Itoa(part.PartNumber))

			// 生成预签名URL
			partURL, err := c.core.Presign(egCtx, "PUT", bucket, object, expires, urlValues)
			if err != nil {
				return &MultipartError{
					Bucket:     bucket,
					Object:     object,
					UploadID:   uploadID,
					PartNumber: part.PartNumber,
					Err:        fmt.Errorf("failed to presign part URL: %w", err),
				}
			}

			part.Url = partURL

			// 发送结果
			select {
			case resultCh <- part:
			case <-egCtx.Done():
				return egCtx.Err()
			}

			return nil
		})
	}

	// 等待所有 goroutine 完成
	go func() {
		eg.Wait()
		close(resultCh)
	}()

	// 收集结果
	for part := range resultCh {
		result.Parts = append(result.Parts, part)
	}

	// 检查是否有错误
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// 按分片编号排序
	sort.Slice(result.Parts, func(i, j int) bool {
		return result.Parts[i].PartNumber < result.Parts[j].PartNumber
	})

	return result, nil
}
