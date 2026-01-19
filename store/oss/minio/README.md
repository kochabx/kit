# MinIO Object Storage Client

生产级 MinIO 对象存储客户端，基于 minio-go/v7 封装，提供简洁易用的 API 和完善的错误处理。

## 特性

- ✅ 完整的桶操作（创建、删除、检查）
- ✅ 简单文件上传（预签名URL）
- ✅ 分片上传支持（支持大文件）
- ✅ 断点续传（自动检测未完成的上传）
- ✅ 并发控制和性能优化
- ✅ 完善的错误处理和类型定义
- ✅ 接口化设计，易于测试和扩展
- ✅ 上下文超时控制
- ✅ 灵活的配置选项

## 安装

```bash
go get github.com/kochabx/kit/store/oss/minio
```

## 快速开始

### 创建客户端

```go
import "github.com/kochabx/kit/store/oss/minio"

// 使用默认配置
client, err := minio.NewClient(
    "localhost:9000",
    "your-access-key",
    "your-secret-key",
    minio.WithUseSSL(false),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

### 桶操作

```go
ctx := context.Background()

// 创建桶
err := client.CreateBucket(ctx, "my-bucket")
if err != nil {
    log.Fatal(err)
}

// 检查桶是否存在
exists, err := client.BucketExists(ctx, "my-bucket")

// 删除桶
err = client.DeleteBucket(ctx, "my-bucket")
```

### 简单上传

```go
// 获取预签名上传URL
params := &minio.PresignedUploadParams{
    Bucket:  "my-bucket",
    Object:  "file.txt",
    Expires: 1 * time.Hour,
}

result, err := client.GetPresignedUploadURL(ctx, params)
if err != nil {
    log.Fatal(err)
}

// 使用 result.URL 进行上传（客户端直传）
// PUT request to result.URL
```

### 分片上传

```go
// 1. 初始化分片上传
params := &minio.InitiateMultipartParams{
    Bucket:     "my-bucket",
    Object:     "large-file.bin",
    ObjectSize: 100 * 1024 * 1024, // 100MB
    PartSize:   10 * 1024 * 1024,  // 10MB per part
}

result, err := client.InitiateMultipartUpload(ctx, params)
if err != nil {
    log.Fatal(err)
}

// 2. 上传每个分片
// 客户端使用 result.Parts[i].URL 上传对应的分片数据

// 3. 完成上传
completeParams := &minio.CompleteMultipartParams{
    Bucket:   "my-bucket",
    Object:   "large-file.bin",
    UploadID: result.UploadID,
}

uploadInfo, err := client.CompleteMultipartUpload(ctx, completeParams)
if err != nil {
    // 如果失败，可以取消上传
    _ = client.AbortMultipartUpload(ctx, &minio.AbortMultipartParams{
        Bucket:   "my-bucket",
        Object:   "large-file.bin",
        UploadID: result.UploadID,
    })
    log.Fatal(err)
}
```

## 配置选项

```go
client, err := minio.NewClient(
    endpoint,
    accessKey,
    secretKey,
    // 基础配置
    minio.WithUseSSL(true),
    minio.WithRegion("us-east-1"),
    
    // 性能配置
    minio.WithMaxConcurrency(20),
    minio.WithRequestTimeout(30 * time.Second),
    minio.WithMaxRetries(3),
    
    // 分片配置
    minio.WithPartSize(10 * 1024 * 1024),
    minio.WithPresignExpiry(1 * time.Hour),
    
    // 自定义 HTTP 客户端
    minio.WithHTTPClient(&http.Client{
        Timeout: 60 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:        100,
            MaxIdleConnsPerHost: 10,
        },
    }),
)
```

## 错误处理

```go
err := client.CreateBucket(ctx, "my-bucket")
if err != nil {
    // 检查特定错误类型
    if errors.Is(err, minio.ErrBucketExists) {
        log.Println("bucket already exists")
    }
    
    // 类型断言获取详细信息
    var bucketErr *minio.BucketError
    if errors.As(err, &bucketErr) {
        log.Printf("Bucket: %s, Operation: %s", bucketErr.Bucket, bucketErr.Operation)
    }
}
```

## 最佳实践

### 1. 使用上下文超时

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := client.CreateBucket(ctx, "my-bucket")
```

### 2. 合理设置分片大小

```go
// 根据文件大小动态调整分片大小
func calculatePartSize(fileSize uint64) uint64 {
    const minPartSize = 5 * 1024 * 1024  // 5MB
    const maxPartSize = 5 * 1024 * 1024 * 1024 // 5GB
    const maxParts = 10000
    
    partSize := fileSize / maxParts
    if partSize < minPartSize {
        partSize = minPartSize
    }
    if partSize > maxPartSize {
        partSize = maxPartSize
    }
    return partSize
}
```

### 3. 处理断点续传

```go
result, err := client.InitiateMultipartUpload(ctx, params)
if err != nil {
    return err
}

// result.Parts 中 Completed=true 的分片已上传，可跳过
for _, part := range result.Parts {
    if part.Completed {
        continue // 跳过已完成的分片
    }
    // 上传该分片
}
```

### 4. 资源清理

```go
result, err := client.InitiateMultipartUpload(ctx, params)
if err != nil {
    return err
}

// 确保失败时清理资源
defer func() {
    if err != nil {
        _ = client.AbortMultipartUpload(ctx, &minio.AbortMultipartParams{
            Bucket:   params.Bucket,
            Object:   params.Object,
            UploadID: result.UploadID,
        })
    }
}()

// ... 上传逻辑
```

## 性能优化

1. **并发控制**: 通过 `WithMaxConcurrency` 控制并发上传的分片数量
2. **连接复用**: 使用自定义 HTTP 客户端配置连接池
3. **内存优化**: 预分配 slice 容量，避免多次扩容
4. **超时控制**: 合理设置请求超时，避免长时间等待
