package redact

import "bytes"

// Mask defines how a matched sensitive value is rendered.
type Mask interface {
	Append(dst, value []byte) []byte
}

type replaceMask []byte

func (m replaceMask) Append(dst, _ []byte) []byte { return append(dst, m...) }

// Replace replaces the complete value.
func Replace(replacement string) Mask { return replaceMask(replacement) }

type keepEdgesMask struct {
	prefix int
	suffix int
	mask   []byte
}

type emailMask struct{}

// Email preserves the email domain and the edges of the local part.
func Email() Mask { return emailMask{} }

func (emailMask) Append(dst, value []byte) []byte {
	at := bytes.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 {
		return append(dst, "******"...)
	}
	local := value[:at]
	if len(local) == 1 {
		dst = append(dst, local[0])
		dst = append(dst, "***"...)
	} else {
		dst = append(dst, local[0])
		dst = append(dst, "***"...)
		dst = append(dst, local[len(local)-1])
	}
	return append(dst, value[at:]...)
}

// KeepEdges preserves prefix and suffix bytes and masks the middle. Values too
// short to hide safely are replaced completely.
func KeepEdges(prefix, suffix int) Mask {
	if prefix < 0 || suffix < 0 {
		panic("redact: negative edge length")
	}
	return keepEdgesMask{prefix: prefix, suffix: suffix, mask: []byte("****")}
}

func (m keepEdgesMask) Append(dst, value []byte) []byte {
	if len(value) <= m.prefix+m.suffix {
		return append(dst, bytes.Repeat([]byte{'*'}, max(len(value), 6))...)
	}
	dst = append(dst, value[:m.prefix]...)
	dst = append(dst, m.mask...)
	return append(dst, value[len(value)-m.suffix:]...)
}
