package redact

import "io"

// Writer 包装 writer 以支持脱敏
type Writer struct {
	writer   io.Writer
	redactor *Redactor
}

// NewWriter 创建脱敏 writer
func NewWriter(writer io.Writer, redactor *Redactor) *Writer {
	if writer == nil {
		panic("writer cannot be nil")
	}
	if redactor == nil {
		panic("redactor cannot be nil")
	}

	return &Writer{
		writer:   writer,
		redactor: redactor,
	}
}

// Write 实现 io.Writer 接口
func (w *Writer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// 快速路径：如果没有规则，直接写入
	if !w.redactor.HasRules() {
		return w.writer.Write(p)
	}
	redacted, changed := w.redactor.Append(nil, p)
	if !changed {
		return w.writer.Write(p)
	}
	written, err := w.writer.Write(redacted)
	if err != nil {
		return 0, err
	}
	if written != len(redacted) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}
