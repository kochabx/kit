package writer

import (
	"io"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileValidatesConfig(t *testing.T) {
	tests := []FileConfig{
		{},
		{
			Path:       filepath.Join(t.TempDir(), "app.log"),
			RotateMode: RotateModeTime,
			TimeRotate: TimeRotateConfig{
				MaxAge:   time.Hour,
				Interval: -time.Second,
			},
		},
		{
			Path:       filepath.Join(t.TempDir(), "app.log"),
			RotateMode: RotateModeSize,
			SizeRotate: SizeRotateConfig{
				MaxSize: -1,
			},
		},
	}

	for i, config := range tests {
		if _, err := NewFile(config); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestNewFileCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	w, err := NewFile(FileConfig{
		Path:       filepath.Join(dir, "app.log"),
		RotateMode: RotateModeSize,
		SizeRotate: SizeRotateConfig{
			MaxSize:    1,
			MaxBackups: 1,
			MaxAge:     1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if closer, ok := w.(io.Closer); ok {
		defer closer.Close()
	}
	if _, err := w.Write([]byte("test\n")); err != nil {
		t.Fatal(err)
	}
}
