package log

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNestedGroupRedactionAndDynamicRules(t *testing.T) {
	redactor, err := NewRedactor(
		FieldRule("password", MaskFull("******")),
		FieldRule("phone", MaskPartial(3, 4)),
		ContentRule("email", `\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, MaskEmail()),
	)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	jsonLogger := wrapHandler(
		slog.NewJSONHandler(&buf, nil),
		&struct {
			loggerOptions
			levelVar *slog.LevelVar
		}{redactor: redactor},
		nil,
	)

	// Nested slog.Group attributes must all be redacted (no outer-key bypass).
	jsonLogger.Info("contact alice@example.com",
		slog.Group("user",
			slog.String("name", "alice"),
			slog.Group("credentials",
				slog.String("password", "super-secret"),
				slog.String("phone", "13812345678"),
			),
		),
	)

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal log JSON: %v (%s)", err, buf.String())
	}
	if decoded["msg"] != "contact a***e@example.com" {
		t.Fatalf("msg = %v, want email redacted", decoded["msg"])
	}
	user := decoded["user"].(map[string]any)
	creds := user["credentials"].(map[string]any)
	if creds["password"] != "******" || creds["phone"] != "138****5678" {
		t.Fatalf("nested credentials not redacted: %+v", creds)
	}

	// Dynamic DisableRule + EnableRule + AddRule + RemoveRule.
	if !redactor.DisableRule("password") {
		t.Fatal("DisableRule returned false")
	}
	buf.Reset()
	jsonLogger.Info("after disable", slog.String("password", "visible"))
	if !strings.Contains(buf.String(), `"password":"visible"`) {
		t.Fatalf("expected unredacted password after DisableRule, got %s", buf.String())
	}
	if !redactor.EnableRule("password") {
		t.Fatal("EnableRule returned false")
	}
	if err := redactor.AddRule(FieldRule("token", MaskFull())); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	jsonLogger.Info("after enable+add", slog.String("password", "p"), slog.String("token", "t"))
	line := buf.String()
	if !strings.Contains(line, `"password":"******"`) || !strings.Contains(line, `"token":"******"`) {
		t.Fatalf("unexpected redaction line: %s", line)
	}
	if !redactor.RemoveRule("token") || redactor.RemoveRule("missing") {
		t.Fatal("RemoveRule behavior mismatch")
	}
}

func TestWithAttrsAndValidation(t *testing.T) {
	var buf bytes.Buffer
	redactor, err := NewRedactor(FieldRule("api_key", MaskPartial(2, 2)))
	if err != nil {
		t.Fatal(err)
	}
	logger := New(WithWriter(&buf), WithNoColor(true), WithRedactor(redactor))
	child := logger.With("api_key", "abcdef12")
	child.Info("login", slog.String("user", "alice"))
	out := buf.String()
	if strings.Contains(out, "abcdef12") {
		t.Fatalf("api_key leaked in log: %s", out)
	}
	if !strings.Contains(out, "ab****12") {
		t.Fatalf("expected masked values in log: %s", out)
	}

	for _, bad := range []Rule{
		FieldRule("", MaskFull()),
		FieldRule("k", nil),
		ContentRule("bad", "[", MaskFull()),
	} {
		if _, err := NewRedactor(bad); err == nil {
			t.Fatalf("expected error for invalid rule %+v", bad)
		}
	}
}

func TestTintConsoleFormatAndContextExtractor(t *testing.T) {
	type ctxKey struct{}
	var buf bytes.Buffer
	logger := New(
		WithWriter(&buf),
		WithLevel(slog.LevelDebug),
		WithTimeFormat("2006-01-02 15:04:05"),
		WithAddSource(true),
		WithContextExtractor(func(ctx context.Context) []slog.Attr {
			if id, ok := ctx.Value(ctxKey{}).(string); ok {
				return []slog.Attr{slog.String("trace_id", id)}
			}
			return nil
		}),
	)

	ctx := context.WithValue(t.Context(), ctxKey{}, "tr-001")
	logger.DebugContext(ctx, "http server listening", slog.String("addr", ":8080"))
	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI color codes from tint, got %q", out)
	}
	if !strings.Contains(out, "trace_id=") || !strings.Contains(out, "tr-001") || !strings.Contains(out, "log_test.go:") {
		t.Fatalf("missing context attr or source line: %q", out)
	}
}

func TestFileMultiHandlerAndRotationCloseZeroLeaks(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() {
		runtime.GC()
		if got := pprof.Lookup("goroutineleak").Count(); got > baseline {
			t.Fatalf("goroutine leak after closing timberjack loggers: got=%d baseline=%d", got, baseline)
		}
	})

	var consoleBuf bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "nested", "app.log")
	redactor, err := NewRedactor(FieldRule("password", MaskFull()))
	if err != nil {
		t.Fatal(err)
	}

	multi, err := NewMulti(
		FileConfig{
			Filename:         logPath,
			MaxSize:          10,
			MaxBackups:       3,
			MaxAge:           7,
			RotationInterval: time.Hour,
			Compression:      "gzip",
		},
		WithWriter(&consoleBuf),
		WithNoColor(true),
		WithRedactor(redactor),
	)
	if err != nil {
		t.Fatal(err)
	}

	multi.Info("service started", slog.String("password", "top-secret"))
	if err := multi.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := multi.Close(); err != nil {
		t.Fatalf("idempotent second Close failed: %v", err)
	}

	fileBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(fileBytes), `"password":"******"`) {
		t.Fatalf("file log missing redacted JSON line: %s", fileBytes)
	}
	if !strings.Contains(consoleBuf.String(), "password=******") {
		t.Fatalf("console log missing redacted line: %s", consoleBuf.String())
	}
	if _, err := NewFile(FileConfig{}); err == nil {
		t.Fatal("expected error for empty Filename")
	}
}

func TestRedactionKindAnyWithGroupDynamicWithAttrsUTF8AndChildClose(t *testing.T) {
	type payload struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	var buf bytes.Buffer
	redactor, err := NewRedactor(
		FieldRule("password", MaskFull()),
		FieldRule("credentials", MaskFull()),
		ContentRule("secret_token", `sk-[a-z0-9]{6}`, MaskPartial(2, 2)),
	)
	if err != nil {
		t.Fatal(err)
	}

	logger := New(WithWriter(&buf), WithNoColor(true), WithRedactor(redactor))
	// Dynamic WithAttrs child logger sees newly added rules.
	child := logger.With("later_secret", "raw-value")
	if err := redactor.AddRule(FieldRule("later_secret", MaskFull())); err != nil {
		t.Fatal(err)
	}
	// Duplicate case-insensitive FieldRule is rejected, and DisableRule/RemoveRule are case-insensitive.
	if err := redactor.AddRule(FieldRule("PASSWORD", MaskPartial(1, 1))); err == nil {
		t.Fatal("expected case-insensitive duplicate FieldRule error")
	}
	if !redactor.DisableRule("PASSWORD") || !redactor.EnableRule("password") {
		t.Fatal("expected case-insensitive DisableRule/EnableRule to succeed")
	}

	// KindAny: error, map[string]any, struct, and WithGroup matching FieldRule.
	child.Error("request failed",
		slog.Any("err", errors.New("bad token sk-abc123")),
		slog.Any("req", map[string]any{"password": "m1", "note": "sk-xyz999"}),
		slog.Any("user", payload{Password: "m2", Email: "alice@example.com"}),
	)
	logger.WithGroup("credentials").Info("grouped", slog.String("token", "leak-me"))
	// Child Close must not close parent rotator/closeFn.
	var parentClosed bool
	rootWithClose := &Logger{slog: logger.slog, levelVar: logger.levelVar, redactor: redactor, closeFn: func() error { parentClosed = true; return nil }}
	_ = rootWithClose.With("k", "v").Close()
	_ = rootWithClose.WithGroup("g").Close()
	if parentClosed {
		t.Fatal("child Close closed parent closeFn")
	}

	out := buf.String()
	for _, leaked := range []string{"raw-value", "sk-abc123", "m1", "sk-xyz999", "m2", "leak-me"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("sensitive value %q leaked in output: %s", leaked, out)
		}
	}

	// UTF-8 MaskPartial & MaskEmail preserve runes without corrupting multi-byte sequences.
	if got := MaskEmail().Apply("张三@example.com"); got != "张***三@example.com" {
		t.Fatalf("MaskEmail UTF-8 = %q, want 张***三@example.com", got)
	}
	if got := MaskPartial(-1, 2).Apply("ab"); got != "******" {
		t.Fatalf("MaskPartial short/negative = %q, want ******", got)
	}
	if got := MaskPartial(-1, 1).Apply("abc"); got != "****c" {
		t.Fatalf("MaskPartial clamped negative = %q, want ****c", got)
	}
	if got := MaskPartial(1, 1).Apply("密码安全"); got != "密****全" {
		t.Fatalf("MaskPartial UTF-8 = %q, want 密****全", got)
	}
}

func TestLogOutputPreview(t *testing.T) {
	t.Run("ConsoleColoredWithCaller", func(t *testing.T) {
		var buf bytes.Buffer
		logger := New(
			WithWriter(&buf),
			WithLevel(slog.LevelDebug),
			WithAddSource(true),
		)

		logger.Debug("loading route table", slog.Int("routes", 12))
		logger.Info("http server listening",
			slog.String("addr", ":8080"),
			slog.Group("tls", slog.Bool("enabled", true), slog.String("min_version", "TLS1.3")),
		)
		logger.Warn("slow query detected", slog.Duration("cost", 240*time.Millisecond))
		logger.Error("request failed", slog.Any("err", errors.New("connection refused")))

		out := buf.String()
		if out == "" || !strings.Contains(out, "log_test.go:") {
			t.Fatalf("expected non-empty output with caller source, got %q", out)
		}
		t.Log("\n" + out)
	})

	t.Run("Redaction", func(t *testing.T) {
		var buf bytes.Buffer
		redactor, err := NewRedactor(
			FieldRule("password", MaskFull("******")),
			FieldRule("phone", MaskPartial(3, 4)),
			ContentRule("email", `\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, MaskEmail()),
		)
		if err != nil {
			t.Fatal(err)
		}
		logger := New(
			WithWriter(&buf),
			WithNoColor(true),
			WithRedactor(redactor),
		)

		logger.Info("user sign-in for alice@example.com",
			slog.String("username", "alice"),
			slog.Group("credentials",
				slog.String("password", "super-secret-pass"),
				slog.String("phone", "13812345678"),
			),
		)

		out := buf.String()
		for _, secret := range []string{"super-secret-pass", "13812345678", "alice@example.com"} {
			if strings.Contains(out, secret) {
				t.Fatalf("secret %q leaked in output: %s", secret, out)
			}
		}
		t.Log("\n" + out)
	})

	t.Run("ContextExtractor", func(t *testing.T) {
		type traceKey struct{}
		type userKey struct{}

		var buf bytes.Buffer
		logger := New(
			WithWriter(&buf),
			WithNoColor(true),
			WithAddSource(true),
			WithContextExtractor(func(ctx context.Context) []slog.Attr {
				var attrs []slog.Attr
				if traceID, ok := ctx.Value(traceKey{}).(string); ok {
					attrs = append(attrs, slog.String("trace_id", traceID))
				}
				if userID, ok := ctx.Value(userKey{}).(int); ok {
					attrs = append(attrs, slog.Int("user_id", userID))
				}
				return attrs
			}),
		)

		ctx := context.WithValue(t.Context(), traceKey{}, "4bf92f3577b34da6a3ce929d0e0e4736")
		ctx = context.WithValue(ctx, userKey{}, 42)
		logger.InfoContext(ctx, "order created", slog.String("order_id", "ord-1001"))
		logger.ErrorContext(ctx, "payment timeout", slog.Duration("timeout", 3*time.Second))

		out := buf.String()
		if !strings.Contains(out, "trace_id=") || !strings.Contains(out, "user_id=42") || !strings.Contains(out, "log_test.go:") {
			t.Fatalf("missing extracted context attrs or caller source: %q", out)
		}
		t.Log("\n" + out)
	})

	t.Run("JSONFileWithCaller", func(t *testing.T) {
		logFile := filepath.Join(t.TempDir(), "app.log")
		redactor, err := NewRedactor(FieldRule("token", MaskPartial(4, 4)))
		if err != nil {
			t.Fatal(err)
		}
		logger, err := NewFile(
			FileConfig{Filename: logFile, MaxSize: 10},
			WithAddSource(true),
			WithRedactor(redactor),
		)
		if err != nil {
			t.Fatal(err)
		}

		logger.Info("service ready",
			slog.String("env", "prod"),
			slog.String("token", "tok_live_1234567890"),
		)
		logger.Warn("cache miss", slog.String("key", "session:42"))
		if err := logger.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("read log file: %v", err)
		}
		out := string(data)
		if !strings.Contains(out, `"source"`) || strings.Contains(out, "tok_live_1234567890") {
			t.Fatalf("unexpected JSON file output: %s", out)
		}
		firstLine, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
		var decoded map[string]any
		if err := json.Unmarshal([]byte(firstLine), &decoded); err != nil {
			t.Fatalf("unmarshal JSON log line: %v (%s)", err, firstLine)
		}
		ts, _ := decoded["time"].(string)
		if _, err := time.Parse(time.DateTime, ts); err != nil {
			t.Fatalf("JSON time %q does not match time.DateTime (%q): %v", ts, time.DateTime, err)
		}
		t.Log("\n" + out)
	})

	t.Run("JSONCustomTimeFormat", func(t *testing.T) {
		customLayout := "2006/01/02 15:04:05"
		logFile := filepath.Join(t.TempDir(), "custom.log")
		logger, err := NewFile(FileConfig{Filename: logFile, MaxSize: 10}, WithTimeFormat(customLayout))
		if err != nil {
			t.Fatal(err)
		}
		logger.Info("custom time sample")
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(data), &decoded); err != nil {
			t.Fatalf("unmarshal custom JSON log: %v (%s)", err, data)
		}
		ts, _ := decoded["time"].(string)
		if _, err := time.Parse(customLayout, ts); err != nil {
			t.Fatalf("custom JSON time %q does not match %q: %v", ts, customLayout, err)
		}
	})

	t.Run("GlobalLogger", func(t *testing.T) {
		prev := Default()
		t.Cleanup(func() { SetDefault(prev) })

		var buf bytes.Buffer
		SetDefault(New(
			WithWriter(&buf),
			WithLevel(slog.LevelDebug),
			WithAddSource(true),
		))

		Info("global package log ready", slog.String("mode", "direct"))
		Error("global error sample", slog.Any("err", errors.New("upstream timeout")))

		out := buf.String()
		if !strings.Contains(out, "log_test.go:") {
			t.Fatalf("expected caller source in global log output, got %q", out)
		}
		t.Log("\n" + out)
	})
}

func TestGlobalLoggerCallerAndConcurrency(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default() must be initialized by init()")
	}
	prev := Default()
	t.Cleanup(func() { SetDefault(prev) })

	SetDefault(nil)
	if Default() != prev {
		t.Fatal("SetDefault(nil) must be a no-op")
	}

	type ctxKey struct{}
	var buf bytes.Buffer
	SetDefault(New(
		WithWriter(&buf),
		WithLevel(slog.LevelDebug),
		WithNoColor(true),
		WithAddSource(true),
		WithContextExtractor(func(ctx context.Context) []slog.Attr {
			if id, ok := ctx.Value(ctxKey{}).(string); ok {
				return []slog.Attr{slog.String("trace_id", id)}
			}
			return nil
		}),
	))

	ctx := context.WithValue(t.Context(), ctxKey{}, "trace-global-1")
	Debug("global debug")
	Info("global info")
	Warn("global warn")
	Error("global error")
	DebugContext(ctx, "ctx debug")
	InfoContext(ctx, "ctx info")
	WarnContext(ctx, "ctx warn")
	ErrorContext(ctx, "ctx error")
	With("component", "worker").Info("with child")
	WithGroup("http").Info("grouped child", slog.Int("status", 200))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 log lines, got %d:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		if !strings.Contains(line, "log_test.go:") {
			t.Fatalf("line %d missing caller log_test.go: %q", i, line)
		}
		if strings.Contains(line, "global.go:") || strings.Contains(line, "logger.go:") {
			t.Fatalf("line %d reported wrapper caller instead of call site: %q", i, line)
		}
	}
	if !strings.Contains(buf.String(), "trace_id=trace-global-1") {
		t.Fatalf("missing extracted context attribute: %s", buf.String())
	}

	buf.Reset()
	SetLevel(slog.LevelWarn)
	Info("suppressed info")
	Warn("visible warn")
	if strings.Contains(buf.String(), "suppressed info") || !strings.Contains(buf.String(), "visible warn") {
		t.Fatalf("SetLevel did not filter properly: %s", buf.String())
	}

	// Concurrent SetDefault and logging under -race.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				SetDefault(New(WithWriter(io.Discard), WithAddSource(true)))
			}
		})
		wg.Go(func() {
			for range 50 {
				Info("concurrent global log", slog.Int("n", 1))
			}
		})
	}
	wg.Wait()
}

func TestRedactionMaxDepthFailClosed(t *testing.T) {
	redactor, err := NewRedactor(FieldRule("password", MaskFull("******")))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	jsonLogger := wrapHandler(
		slog.NewJSONHandler(&buf, nil),
		&struct {
			loggerOptions
			levelVar *slog.LevelVar
		}{redactor: redactor},
		nil,
	)

	// Build a 19-level nested map+struct payload so fields at depth <= 16 are redacted
	// normally while anything past maxRedactDepth (16) is fail-closed truncated.
	type deepLeaf struct {
		Password string `json:"password"`
		Note     string `json:"note"`
	}
	cur := any(deepLeaf{Password: "ultra-secret-beyond-limit", Note: "deep"})
	for range 17 {
		cur = map[string]any{"child": cur}
	}
	root := map[string]any{
		"password": "top-level-secret",
		"nested":   cur,
	}

	// Boundary check: depth == 16 still traverses and redacts fields directly.
	boundary := any(deepLeaf{Password: "boundary-secret-at-16", Note: "ok"})
	for range 15 {
		boundary = map[string]any{"child": boundary}
	}

	jsonLogger.Info("deep payload",
		slog.Any("overflow", root),
		slog.Any("boundary", boundary),
	)

	out := buf.String()
	for _, leaked := range []string{"top-level-secret", "boundary-secret-at-16", "ultra-secret-beyond-limit"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("secret %q leaked past depth cap in JSON output: %s", leaked, out)
		}
	}
	if !strings.Contains(out, redactedMaxDepth) {
		t.Fatalf("expected fail-closed marker %q in output: %s", redactedMaxDepth, out)
	}
	if !strings.Contains(out, `"password":"******"`) {
		t.Fatalf("expected redacted boundary password in output: %s", out)
	}
}

func TestMaskPartialOverflowAndUniqueRuleNames(t *testing.T) {
	// Overflow-safe boundary cases and multibyte runes.
	if got := MaskPartial(math.MaxInt, 1).Apply("a"); got != "******" {
		t.Fatalf("MaskPartial(MaxInt, 1) = %q, want ******", got)
	}
	if got := MaskPartial(2, math.MaxInt).Apply("abcdef"); got != "******" {
		t.Fatalf("MaskPartial(2, MaxInt) = %q, want ******", got)
	}
	if got := MaskPartial(2, 2).Apply("abcd"); got != "******" {
		t.Fatalf("MaskPartial exact sum = %q, want ******", got)
	}
	if got := MaskPartial(1, 1).Apply("安全码"); got != "安****码" {
		t.Fatalf("MaskPartial 3-rune UTF-8 = %q, want 安****码", got)
	}

	// Same-batch cross-kind duplicate (case-folded) must be rejected.
	if _, err := NewRedactor(
		FieldRule("token", MaskFull()),
		ContentRule("Token", `tok_[0-9]+`, MaskFull()),
	); err == nil {
		t.Fatal("expected duplicate rule error for same-batch FieldRule and ContentRule")
	}

	// Separate-batch FieldRule -> ContentRule conflict preserves existing FieldRule.
	r1, err := NewRedactor(FieldRule("token", MaskFull("F")))
	if err != nil {
		t.Fatal(err)
	}
	if err := r1.AddRule(ContentRule("Token", `tok_[0-9]+`, MaskFull("C"))); err == nil {
		t.Fatal("expected duplicate rule error adding ContentRule matching existing FieldRule")
	}
	var buf1 bytes.Buffer
	New(WithWriter(&buf1), WithNoColor(true), WithRedactor(r1)).Info("tok_123", slog.String("token", "secret"))
	if !strings.Contains(buf1.String(), "token=F") || !strings.Contains(buf1.String(), "tok_123") {
		t.Fatalf("existing FieldRule affected after failed AddRule: %s", buf1.String())
	}

	// Reverse order ContentRule -> FieldRule conflict preserves existing ContentRule and allows management by name.
	r2, err := NewRedactor(ContentRule("token", `tok_[0-9]+`, MaskFull("C")))
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.AddRule(FieldRule("TOKEN", MaskFull("F"))); err == nil {
		t.Fatal("expected duplicate rule error adding FieldRule matching existing ContentRule")
	}
	var buf2 bytes.Buffer
	l2 := New(WithWriter(&buf2), WithNoColor(true), WithRedactor(r2))
	l2.Info("seen tok_456")
	if !strings.Contains(buf2.String(), "seen C") {
		t.Fatalf("existing ContentRule did not apply: %s", buf2.String())
	}
	if !r2.DisableRule("TOKEN") {
		t.Fatal("expected DisableRule on ContentRule by case-folded name to succeed")
	}
	buf2.Reset()
	l2.Info("seen tok_789")
	if !strings.Contains(buf2.String(), "tok_789") {
		t.Fatalf("ContentRule remained active after DisableRule: %s", buf2.String())
	}
	if !r2.EnableRule("token") || !r2.RemoveRule("Token") {
		t.Fatal("expected EnableRule and RemoveRule on ContentRule to succeed")
	}
}

func TestNonStringKeyMapRedaction(t *testing.T) {
	redactor, err := NewRedactor(FieldRule("password", MaskFull("******")))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	jsonLogger := wrapHandler(
		slog.NewJSONHandler(&buf, nil),
		&struct {
			loggerOptions
			levelVar *slog.LevelVar
		}{redactor: redactor},
		nil,
	)

	type account struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	jsonLogger.Info("non-string map",
		slog.Any("by_int", map[int]account{
			1: {Name: "alice", Password: "int-map-secret-password"},
		}),
		slog.Any("by_uint", map[uint64]map[string]any{
			42: {"password": "uint-map-secret-password", "role": "admin"},
		}),
	)

	out := buf.String()
	for _, leaked := range []string{"int-map-secret-password", "uint-map-secret-password"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("secret %q leaked from non-string-key map: %s", leaked, out)
		}
	}
	if strings.Count(out, `"password":"******"`) != 2 {
		t.Fatalf("expected 2 redacted passwords in JSON log, got: %s", out)
	}
}
