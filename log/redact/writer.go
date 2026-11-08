package redact

import "io"

// Writer wraps an output writer with redaction.
type Writer struct {
	writer   io.Writer
	redactor *Redactor
}

// NewWriter creates a redacting writer.
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

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
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
