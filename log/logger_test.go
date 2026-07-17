package log

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	kiterrors "github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log/redact"
	"github.com/kochabx/kit/log/writer"
	"github.com/rs/zerolog"
)

type mock struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestSetGlobalConcurrentAccess(t *testing.T) {
	original := Global()
	t.Cleanup(func() { SetGlobal(original) })

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				_ = Global()
				_ = Info()
			}
		})
	}
	for range 100 {
		SetGlobal(New(WithLevel(zerolog.Disabled)))
	}
	wg.Wait()
}

func TestSetGlobalRejectsNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	SetGlobal(nil)
}

func (m *mock) String() string {
	bytes, _ := json.Marshal(m)
	return string(bytes)
}

func TestLog(t *testing.T) {
	logger := New()
	logger.Debug().Msg("test debug message")
	logger.Info().Str("key", "value").Msg("test info with field")
	logger.Error().Err(kiterrors.New(400, "test")).Msg("test error")

	m := &mock{Name: "test", Age: 10}
	logger.Info().Any("user", m).Msg("test with struct")
}

func TestGlobalLog(t *testing.T) {
	Debug().Msg("test global debug log")
	Info().Msg("test global info log")
	Warn().Msg("test global warn log")
	Warn().Err(kiterrors.New(404, "test warn error")).Msg("test global warn error log")
	Error().Err(kiterrors.New(500, "test global error")).Msg("test global error log")
}

func TestFileLog(t *testing.T) {
	config := writer.FileConfig{
		Path:       filepath.Join(t.TempDir(), "test.log"),
		RotateMode: writer.RotateModeSize,
		SizeRotate: writer.SizeRotateConfig{
			MaxSize:    10,
			MaxBackups: 3,
			MaxAge:     7,
			Compress:   true,
		},
	}

	logger, err := NewFile(config)
	if err != nil {
		t.Fatalf("Failed to create file logger: %v", err)
	}
	defer logger.Close()

	logger.Info().Msg("test file log")
	logger.Info().Str("phone", "1234567890").Msg("test redact phone")
}

func TestMultiLog(t *testing.T) {
	config := writer.FileConfig{
		Path:       filepath.Join(t.TempDir(), "multi.log"),
		RotateMode: writer.RotateModeTime,
		TimeRotate: writer.TimeRotateConfig{
			MaxAge:   24 * time.Hour,
			Interval: time.Hour,
		},
	}

	logger, err := NewMulti(config)
	if err != nil {
		t.Fatalf("Failed to create multi logger: %v", err)
	}
	defer logger.Close()

	logger.Info().Str("type", "multi").Msg("test multi output log")
}

func TestTimeRotateDefaults(t *testing.T) {
	logger, err := NewFile(writer.FileConfig{
		Path:       filepath.Join(t.TempDir(), "default-time.log"),
		RotateMode: writer.RotateModeTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOptionAppliedOnce(t *testing.T) {
	called := 0
	logger := New(
		func(*loggerOptions) { called++ },
		WithRedactor(mustRedactor(t)),
	)
	if logger == nil {
		t.Fatal("expected logger")
	}
	if called != 1 {
		t.Fatalf("expected option to be applied once, got %d", called)
	}
}

func TestCallerOptions(t *testing.T) {
	for _, test := range []struct {
		name string
		opt  Option
		want int
	}{
		{name: "caller", opt: WithCaller(), want: 0},
		{name: "caller skip", opt: WithCallerSkip(2), want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := loggerOptions{}
			test.opt(&options)
			if !options.caller || options.callerSkip != test.want {
				t.Fatalf("caller skip = %v, want %d", options.callerSkip, test.want)
			}
		})
	}
}

func mustRedactor(t *testing.T) *redact.Redactor {
	t.Helper()
	r, err := redact.New()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
