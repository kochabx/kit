package internal

// ZeroPad pads the byte slice to the specified length by prepending zeros.
// If the slice is already longer than or equal to the target length,
// it returns the first 'length' bytes.
//
// This implementation uses a single allocation for better performance.
func ZeroPad(b []byte, length int) []byte {
	if len(b) >= length {
		return b[:length]
	}

	// Single allocation: create target size buffer and copy to the end
	result := make([]byte, length)
	copy(result[length-len(b):], b)
	return result
}
