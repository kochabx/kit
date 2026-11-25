package grpc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/kochabx/kit/fault"
)

type testEchoService interface {
	FailFault(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
	FailRaw(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
	Panic(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
}

type echoImpl struct{}

func (echoImpl) FailFault(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, fault.NotFound("order missing").WithCause(errors.New("secret-db-err"))
}

func (echoImpl) FailRaw(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, errors.New("secret-internal-sql-leak")
}

func (echoImpl) Panic(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	panic("boom")
}

var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: "kit.test.Echo",
	HandlerType: (*testEchoService)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "FailFault",
			Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := new(emptypb.Empty)
				if err := dec(in); err != nil {
					return nil, err
				}
				if interceptor == nil {
					return srv.(testEchoService).FailFault(ctx, in)
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/kit.test.Echo/FailFault"}, func(ctx context.Context, req any) (any, error) {
					return srv.(testEchoService).FailFault(ctx, req.(*emptypb.Empty))
				})
			},
		},
		{
			MethodName: "FailRaw",
			Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := new(emptypb.Empty)
				if err := dec(in); err != nil {
					return nil, err
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/kit.test.Echo/FailRaw"}, func(ctx context.Context, req any) (any, error) {
					return srv.(testEchoService).FailRaw(ctx, req.(*emptypb.Empty))
				})
			},
		},
		{
			MethodName: "Panic",
			Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := new(emptypb.Empty)
				if err := dec(in); err != nil {
					return nil, err
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/kit.test.Echo/Panic"}, func(ctx context.Context, req any) (any, error) {
					return srv.(testEchoService).Panic(ctx, req.(*emptypb.Empty))
				})
			},
		},
	},
}

func TestServerInterceptorsHealthAndZeroLeaks(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() {
		runtime.GC()
		if got := pprof.Lookup("goroutineleak").Count(); got > baseline {
			t.Fatalf("goroutine leak in grpc server: got=%d baseline=%d", got, baseline)
		}
	})

	var logBuf bytes.Buffer
	srv := NewServer(
		WithAddr("127.0.0.1:0"),
		WithLogger(slog.New(slog.NewJSONHandler(&logBuf, nil))),
	)
	srv.GRPCServer().RegisterService(&echoServiceDesc, echoImpl{})

	if err := srv.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop(t.Context()) }()

	if err := srv.HealthCheck(t.Context()); err != nil {
		t.Fatal(err)
	}

	conn, err := grpc.NewClient(srv.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 1. Built-in grpc/health/v1 SERVING check.
	hc := healthpb.NewHealthClient(conn)
	resp, err := hc.Check(t.Context(), &healthpb.HealthCheckRequest{})
	if err != nil || resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health check = (%v, %v)", resp, err)
	}

	// 2. *fault.Error maps to gRPC NotFound and hides internal cause.
	err = conn.Invoke(t.Context(), "/kit.test.Echo/FailFault", &emptypb.Empty{}, &emptypb.Empty{})
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound || st.Message() != "order missing" || strings.Contains(st.Message(), "secret-db-err") {
		t.Fatalf("FailFault status = %v", st)
	}

	// 3. Raw error maps safely to Internal without leaking error string.
	err = conn.Invoke(t.Context(), "/kit.test.Echo/FailRaw", &emptypb.Empty{}, &emptypb.Empty{})
	st, _ = status.FromError(err)
	if st.Code() != codes.Internal || strings.Contains(st.Message(), "secret-internal") {
		t.Fatalf("FailRaw leaked or wrong code: %v", st)
	}

	// 4. Recovery catches panic cleanly as Internal.
	err = conn.Invoke(t.Context(), "/kit.test.Echo/Panic", &emptypb.Empty{}, &emptypb.Empty{})
	st, _ = status.FromError(err)
	if st.Code() != codes.Internal {
		t.Fatalf("Panic status code = %v, want Internal", st.Code())
	}

	_ = conn.Close()
	if err := srv.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logBuf.String(), "grpc panic recovered") {
		t.Fatalf("expected recovery log entry, got %s", logBuf.String())
	}
}

func TestRunBlockingContextCancel(t *testing.T) {
	srv := NewServer(WithAddr("127.0.0.1:0"))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// Poll briefly until listener binds rather than fixed sleep.
	for range 200 {
		if srv.HealthCheck(t.Context()) == nil {
			break
		}
		runtime.Gosched()
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}

	// mapError maps context cancellation/deadline cleanly instead of Internal.
	if st, _ := status.FromError(mapError(context.Canceled)); st.Code() != codes.Canceled {
		t.Fatalf("mapError(Canceled) = %v, want Canceled", st.Code())
	}
	if st, _ := status.FromError(mapError(context.DeadlineExceeded)); st.Code() != codes.DeadlineExceeded {
		t.Fatalf("mapError(DeadlineExceeded) = %v, want DeadlineExceeded", st.Code())
	}
}

func TestServerCannotRestartAfterStop(t *testing.T) {
	srv := NewServer(WithAddr("127.0.0.1:0"))
	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	if err := srv.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := srv.Start(t.Context()); err == nil {
		_ = srv.Stop(t.Context())
		t.Fatal("expected error when restarting stopped gRPC server")
	}
	if err := srv.HealthCheck(t.Context()); err == nil {
		t.Fatal("HealthCheck must fail after Stop")
	}
}
