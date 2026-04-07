package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kochabx/kit/cx"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/transport"
)

var ErrAlreadyRunning = errors.New("application is already running")

// builder — 两阶段构建：先收集配置，New() 中一次性创建容器
type builder struct {
	ctx             context.Context
	shutdownTimeout time.Duration
	signals         []os.Signal
	container       *cx.Container
	cxOpts          []cx.Option
	servers         []transport.Server
	components      []component
}

type component struct {
	key   string
	value any
}

// Option 配置 Application。
type Option func(*builder)

// WithContext 设置应用的根上下文。
func WithContext(ctx context.Context) Option {
	return func(b *builder) {
		if ctx != nil {
			b.ctx = ctx
		}
	}
}

// WithShutdownTimeout 设置优雅关闭的超时时间。
func WithShutdownTimeout(d time.Duration) Option {
	return func(b *builder) {
		if d > 0 {
			b.shutdownTimeout = d
		}
	}
}

// WithSignals 设置触发优雅关闭的系统信号。
func WithSignals(signals ...os.Signal) Option {
	return func(b *builder) {
		if len(signals) > 0 {
			b.signals = make([]os.Signal, len(signals))
			copy(b.signals, signals)
		}
	}
}

// WithContainer 使用自定义 cx 容器替代内部构建的容器。
func WithContainer(c *cx.Container) Option {
	return func(b *builder) {
		if c != nil {
			b.container = c
		}
	}
}

// WithServer 注册一个 transport.Server。
func WithServer(server transport.Server) Option {
	return func(b *builder) {
		if server != nil {
			b.servers = append(b.servers, server)
		}
	}
}

// WithServers 注册多个 transport.Server。
func WithServers(servers ...transport.Server) Option {
	return func(b *builder) {
		for _, s := range servers {
			if s != nil {
				b.servers = append(b.servers, s)
			}
		}
	}
}

// WithComponent 注册一个自定义 cx 组件（可实现 cx.Starter / cx.Stopper / cx.Checker）。
// 组件按注册顺序启动，按反向顺序关闭。
func WithComponent(key string, value any) Option {
	return func(b *builder) {
		if key != "" && value != nil {
			b.components = append(b.components, component{key: key, value: value})
		}
	}
}

// WithOnStart 注册一个在所有组件启动前执行的钩子。
func WithOnStart(fn func(ctx context.Context) error) Option {
	return func(b *builder) {
		if fn != nil {
			b.cxOpts = append(b.cxOpts, cx.WithOnStart(fn))
		}
	}
}

// WithOnStarted 注册一个在所有组件启动后执行的钩子。
func WithOnStarted(fn func(ctx context.Context) error) Option {
	return func(b *builder) {
		if fn != nil {
			b.cxOpts = append(b.cxOpts, cx.WithOnStarted(fn))
		}
	}
}

// WithOnStopping 注册一个在组件停止前执行的钩子。
func WithOnStopping(fn func(ctx context.Context) error) Option {
	return func(b *builder) {
		if fn != nil {
			b.cxOpts = append(b.cxOpts, cx.WithOnStopping(fn))
		}
	}
}

// WithOnStop 注册一个在所有组件停止后执行的钩子。
func WithOnStop(fn func(ctx context.Context) error) Option {
	return func(b *builder) {
		if fn != nil {
			b.cxOpts = append(b.cxOpts, cx.WithOnStop(fn))
		}
	}
}

// ---------------------------------------------------------------------------
// Application
// ---------------------------------------------------------------------------

// Application 管理应用的生命周期，包括启动、停止和健康检查。
type Application struct {
	container       *cx.Container
	ctx             context.Context
	cancel          context.CancelFunc
	shutdownTimeout time.Duration
	signals         []os.Signal
	running         atomic.Bool
}

// New 使用给定选项创建新的应用实例。
func New(options ...Option) *Application {
	b := &builder{
		shutdownTimeout: 30 * time.Second,
		signals:         []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT},
	}

	for _, opt := range options {
		opt(b)
	}

	// 构建容器：优先使用外部容器，否则用收集的 cx.Option 创建
	container := b.container
	if container == nil {
		b.cxOpts = append(b.cxOpts, cx.WithStopTimeout(b.shutdownTimeout))
		container = cx.New(b.cxOpts...)
	}

	// 将 servers 注册为 cx 组件（transport.Server 已实现 cx.Starter/cx.Stopper）
	for i, s := range b.servers {
		key := fmt.Sprintf("app:server:%d", i)
		cx.MustSupply(container, key, s)
	}

	// 注册自定义组件
	for _, comp := range b.components {
		cx.MustSupply(container, comp.key, comp.value)
	}

	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	return &Application{
		container:       container,
		ctx:             ctx,
		cancel:          cancel,
		shutdownTimeout: b.shutdownTimeout,
		signals:         b.signals,
	}
}

// Container 返回底层 cx 容器，可用于注册自定义组件。
func (app *Application) Container() *cx.Container {
	return app.container
}

// HealthCheck 返回所有组件的聚合健康报告。
func (app *Application) HealthCheck(ctx context.Context) *cx.HealthReport {
	return app.container.HealthCheck(ctx)
}

// Run 启动所有组件，阻塞直到收到关闭信号或上下文取消，然后优雅关闭。
func (app *Application) Run() error {
	if !app.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer app.running.Store(false)

	ctx := app.ctx
	defer app.cancel()

	// 启动 cx 容器（构建组件 → onStart 钩子 → Starter.Start 按依赖序）
	if err := app.container.Start(ctx); err != nil {
		return fmt.Errorf("app: start: %w", err)
	}

	// 等待关闭信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, app.signals...)
	defer signal.Stop(quit)

	select {
	case sig := <-quit:
		log.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case <-ctx.Done():
	}

	return app.shutdown()
}

// Shutdown 触发优雅关闭。
func (app *Application) Shutdown() {
	app.cancel()
}

// shutdown 执行优雅关闭流程。
func (app *Application) shutdown() error {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), app.shutdownTimeout)
	defer shutdownCancel()

	if err := app.container.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("shutdown completed with errors")
		return fmt.Errorf("app: shutdown: %w", err)
	}

	return nil
}
