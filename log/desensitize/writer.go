package desensitize

import (
	"io"
	"unsafe"
)

// Writer 包装 writer 以支持脱敏
type Writer struct {
	writer io.Writer
	hook   *Hook
}

// NewWriter 创建脱敏 writer
func NewWriter(writer io.Writer, hook *Hook) *Writer {
	if writer == nil {
		panic("writer cannot be nil")
	}
	if hook == nil {
		panic("hook cannot be nil")
	}

	return &Writer{
		writer: writer,
		hook:   hook,
	}
}

// Write 实现 io.Writer 接口
func (w *Writer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// 快速路径：如果没有规则，直接写入
	if w.hook.RuleCount() == 0 {
		return w.writer.Write(p)
	}

	// 零拷贝转换：p 在本函数调用期间不会被修改，unsafe.String 安全
	text := unsafe.String(unsafe.SliceData(p), len(p))
	desensitized := w.hook.Desensitize(text)

	// 内容未变化，直接写入原始字节
	if desensitized == text {
		return w.writer.Write(p)
	}

	return io.WriteString(w.writer, desensitized)
}
