package log

import (
	"fmt"
	"io"

	"github.com/rs/zerolog"

	"github.com/kochabx/kit/log/writer"
)

// NewFile creates a file logger.
func NewFile(config writer.FileConfig, opts ...Option) (*Logger, error) {
	fileWriter, closer, err := openFile(config)
	if err != nil {
		return nil, err
	}
	logger := newWithWriter(fileWriter, opts...)
	logger.closer = closer
	return logger, nil
}

// NewMulti creates a logger that writes to both file and console.
func NewMulti(config writer.FileConfig, opts ...Option) (*Logger, error) {
	fileWriter, closer, err := openFile(config)
	if err != nil {
		return nil, err
	}
	output := zerolog.MultiLevelWriter(fileWriter, writer.NewConsole())
	logger := newWithWriter(output, opts...)
	logger.closer = closer
	return logger, nil
}

func openFile(config writer.FileConfig) (io.Writer, io.Closer, error) {
	fileWriter, err := writer.NewFile(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create file writer: %w", err)
	}
	if closer, ok := fileWriter.(io.Closer); ok {
		return fileWriter, closer, nil
	}
	return fileWriter, nil, nil
}
