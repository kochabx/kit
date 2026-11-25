package ws

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestOrderedStreamReconnectAndZeroLeaks(t *testing.T) {
	baseline := pprof.Lookup("goroutineleak").Count()
	t.Cleanup(func() {
		runtime.GC()
		if got := pprof.Lookup("goroutineleak").Count(); got > baseline {
			t.Fatalf("goroutine leak in ws client: got=%d baseline=%d", got, baseline)
		}
	})

	var connNum atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		attempt := connNum.Add(1)
		if attempt == 1 {
			// First connection sends two ordered messages, then drops abnormally.
			_ = c.Write(r.Context(), websocket.MessageText, []byte("msg-0"))
			_ = c.Write(r.Context(), websocket.MessageText, []byte("msg-1"))
			_ = c.Close(websocket.StatusInternalError, "simulated drop")
			return
		}
		// Second connection echoes received messages then closes normally.
		_ = c.Write(r.Context(), websocket.MessageText, []byte("msg-reconnected-"+strconv.Itoa(int(attempt))))
		_, data, err := c.Read(r.Context())
		if err == nil {
			_ = c.Write(r.Context(), websocket.MessageText, append([]byte("echo:"), data...))
		}
		_ = c.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := New(
		wsURL,
		WithPingInterval(20*time.Millisecond),
		WithReconnect(true, 5*time.Millisecond, 20*time.Millisecond, 5),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	var received []string
	for msg, err := range client.Messages(t.Context()) {
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		received = append(received, string(msg.Data))
		if string(msg.Data) == "msg-reconnected-2" {
			if err := client.Send(t.Context(), Message{Type: MessageText, Data: []byte("ping-body")}); err != nil {
				t.Fatalf("Send failed: %v", err)
			}
		}
		if len(received) == 4 {
			break
		}
	}

	want := []string{"msg-0", "msg-1", "msg-reconnected-2", "echo:ping-body"}
	if strings.Join(received, ",") != strings.Join(want, ",") {
		t.Fatalf("ordered messages = %v, want %v", received, want)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("idempotent second Close failed: %v", err)
	}

	// WithReconnect(false) returns dial failure cleanly without looping indefinitely.
	noReconn, err := New(wsURL, WithDialTimeout(20*time.Millisecond), WithReconnect(false, 5*time.Millisecond, 10*time.Millisecond, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = noReconn.Close() }()
	srv.Close()
	gotErr := false
	for _, streamErr := range noReconn.Messages(t.Context()) {
		if streamErr != nil {
			gotErr = true
			break
		}
	}
	if !gotErr {
		t.Fatal("expected dial error yielded when reconnect is disabled")
	}
}

type wrappedRoundTripper struct{ next http.RoundTripper }

func (w wrappedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return w.next.RoundTrip(r)
}

func TestNewWithWrappedDefaultTransport(t *testing.T) {
	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })

	http.DefaultTransport = wrappedRoundTripper{next: orig}

	// Without custom TLSConfig, New succeeds without asserting DefaultTransport.
	c, err := New("ws://127.0.0.1:0/ws")
	if err != nil {
		t.Fatalf("New() without TLS failed on wrapped DefaultTransport: %v", err)
	}
	_ = c.Close()

	// With TLSConfig, New returns a clear error instead of panicking.
	if _, err := New("wss://127.0.0.1:0/ws", WithTLSConfig(&tls.Config{})); err == nil {
		t.Fatal("expected error when applying WithTLSConfig to non-*http.Transport DefaultTransport")
	}
}
