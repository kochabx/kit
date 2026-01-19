package ecies

import (
	"sync"
)

// bufferPool is a pool of byte slices used for temporary operations.
// This reduces memory allocations during encryption/decryption.
var bufferPool = sync.Pool{
	New: func() any {
		// Allocate a reasonably sized buffer (1KB default)
		buf := make([]byte, 0, 1024)
		return &buf
	},
}

// getBuffer retrieves a buffer from the pool with at least the specified capacity.
// The returned buffer should be returned to the pool using putBuffer after use.
func getBuffer(minCapacity int) []byte {
	bufPtr := bufferPool.Get().(*[]byte)
	buf := *bufPtr

	// If the pooled buffer is too small, allocate a new one
	if cap(buf) < minCapacity {
		buf = make([]byte, 0, minCapacity)
	}

	// Reset length to 0 but keep capacity
	return buf[:0]
}

// putBuffer returns a buffer to the pool for reuse.
// The buffer should not be used after calling this function.
func putBuffer(buf []byte) {
	// Only return reasonably-sized buffers to the pool
	// to avoid keeping very large buffers in memory
	const maxPooledBufferSize = 64 * 1024 // 64 KB

	if cap(buf) <= maxPooledBufferSize {
		buf = buf[:0] // Reset length
		bufferPool.Put(&buf)
	}
}
