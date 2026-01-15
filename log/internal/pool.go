package internal

import (
	"bytes"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() any {
		return &bytes.Buffer{}
	},
}

// GetBuffer 从池中获取一个 Buffer
func GetBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

// PutBuffer 将 Buffer 归还到池中
func PutBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// 如果 buffer 太大，不放回池中，让 GC 回收
	if buf.Cap() > 64*1024 {
		return
	}
	buf.Reset()
	bufferPool.Put(buf)
}
