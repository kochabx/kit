package app

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kochabx/kit/cx"
	"github.com/kochabx/kit/store/db"
	"github.com/kochabx/kit/transport/http"
)

func init() { gin.SetMode(gin.TestMode) }

func newTestServer() *http.Server {
	return http.NewServer(gin.New())
}

// ---------------------------------------------------------------------------
// 基础创建
// ---------------------------------------------------------------------------

func TestNew_Default(t *testing.T) {
	app := New()
	if app.Container() == nil {
		t.Fatal("expected non-nil container")
	}
}

func TestNew_WithServer(t *testing.T) {
	app := New(WithServer(newTestServer()))

	m := app.Container().Metrics()
	if m.ComponentCount != 1 {
		t.Fatalf("expected 1 component, got %d", m.ComponentCount)
	}
}

func TestNew_WithServers(t *testing.T) {
	app := New(WithServers(newTestServer(), newTestServer()))

	m := app.Container().Metrics()
	if m.ComponentCount != 2 {
		t.Fatalf("expected 2 components, got %d", m.ComponentCount)
	}
}

func TestNew_NilServerIgnored(t *testing.T) {
	app := New(WithServer(nil))

	m := app.Container().Metrics()
	if m.ComponentCount != 0 {
		t.Fatalf("expected 0 components, got %d", m.ComponentCount)
	}
}

// ---------------------------------------------------------------------------
// Run / Stop 生命周期
// ---------------------------------------------------------------------------

func TestRun_NoServers(t *testing.T) {
	app := New()

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(50 * time.Millisecond)
	app.Shutdown()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Shutdown()")
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	app := New(WithContext(ctx))

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancel")
	}
}

func TestRun_DoubleRunReturnsError(t *testing.T) {
	app := New()

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(50 * time.Millisecond)

	// 第二次 Start 应立即返回错误
	err := app.Run()
	if err != ErrAlreadyRunning {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}

	app.Shutdown()
	<-done
}

func TestRun_WithOptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	app := New(
		WithContext(ctx),
		WithShutdownTimeout(5*time.Second),
		WithSignals(os.Interrupt, syscall.SIGTERM),
		WithServer(newTestServer()),
	)

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(100 * time.Millisecond)
	app.Shutdown()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return")
	}
}

// ---------------------------------------------------------------------------
// 生命周期钩子
// ---------------------------------------------------------------------------

func TestRun_LifecycleHooks(t *testing.T) {
	var order []string

	app := New(
		WithOnStart(func(ctx context.Context) error {
			order = append(order, "onStart")
			return nil
		}),
		WithOnStarted(func(ctx context.Context) error {
			order = append(order, "onStarted")
			return nil
		}),
		WithOnStopping(func(ctx context.Context) error {
			order = append(order, "onStopping")
			return nil
		}),
		WithOnStop(func(ctx context.Context) error {
			order = append(order, "onStop")
			return nil
		}),
	)

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(50 * time.Millisecond)
	app.Shutdown()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return")
	}

	expected := []string{"onStart", "onStarted", "onStopping", "onStop"}
	if len(order) != len(expected) {
		t.Fatalf("expected hooks %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("hook[%d]: expected %q, got %q", i, v, order[i])
		}
	}
}

// ---------------------------------------------------------------------------
// WithContainer — 自定义容器
// ---------------------------------------------------------------------------

func TestRun_WithContainer(t *testing.T) {
	dbClient, err := db.New(&db.SQLiteConfig{FilePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}

	c := cx.New()
	cx.MustSupply(c, "db", dbClient)

	app := New(WithContainer(c))

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(50 * time.Millisecond)

	// db.Client.Start 调用 Ping，验证组件已启动
	if dbClient.Ping(context.Background()) != nil {
		t.Fatal("expected db to be reachable after start")
	}

	app.Shutdown()
	<-done
}

// ---------------------------------------------------------------------------
// HealthCheck
// ---------------------------------------------------------------------------

func TestHealthCheck(t *testing.T) {
	dbClient, err := db.New(&db.SQLiteConfig{FilePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}

	c := cx.New()
	cx.MustSupply(c, "db", dbClient)

	app := New(WithContainer(c))

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(50 * time.Millisecond)

	report := app.HealthCheck(context.Background())
	if !report.Healthy {
		t.Fatal("expected healthy report")
	}

	app.Shutdown()
	<-done
}

// ---------------------------------------------------------------------------
// 关闭顺序 — 验证 cx 反向依赖序
// ---------------------------------------------------------------------------

func TestRun_ShutdownOrder(t *testing.T) {
	// 使用真实 HTTP server 和 SQLite DB 验证关闭顺序
	// 注册顺序：server → db → 停止顺序：db → server

	srv := http.NewServer(gin.New(), http.WithAddr(":18999"))

	dbClient, err := db.New(&db.SQLiteConfig{FilePath: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}

	var order []string

	app := New(
		WithServer(srv),
		WithComponent("db", dbClient),
		WithOnStopping(func(ctx context.Context) error {
			// 记录 db 连接状态：如果 db 已关闭，Ping 会失败
			if dbClient.Ping(ctx) != nil {
				order = append(order, "db-closed")
			} else {
				order = append(order, "db-alive")
			}
			return nil
		}),
	)

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	time.Sleep(100 * time.Millisecond)
	app.Shutdown()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return")
	}

	// OnStopping 在组件 Stop 之前执行，此时 db 应该还活着
	if len(order) != 1 || order[0] != "db-alive" {
		t.Fatalf("expected [db-alive], got %v", order)
	}
}
