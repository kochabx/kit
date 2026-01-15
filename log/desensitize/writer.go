package desensitize

import (
	"io"

	"github.com/kochabx/kit/log/internal"
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

	// 使用池化的 buffer
	buf := internal.GetBuffer()
	defer internal.PutBuffer(buf)

	// 安全地转换为字符串
	text := string(p)
	desensitized := w.hook.Desensitize(text)

	// 如果内容没有变化，直接写入原始数据
	if desensitized == text {
		return w.writer.Write(p)
	}

	// 写入脱敏后的数据
	buf.WriteString(desensitized)
	return w.writer.Write(buf.Bytes())
}
