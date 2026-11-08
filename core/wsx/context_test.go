package wsx

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func localWebSocket(t *testing.T) (string, <-chan *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 16)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		accepted <- conn
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(kind, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), accepted
}

func receivePeer(t *testing.T, peers <-chan *websocket.Conn) *websocket.Conn {
	t.Helper()
	select {
	case conn := <-peers:
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	case <-time.After(3 * time.Second):
		t.Fatal("no WebSocket handshake")
		return nil
	}
}

func awaitSessionEnd(t *testing.T, client *Client) {
	t.Helper()
	require.Eventually(t, func() bool {
		client.mu.RLock()
		defer client.mu.RUnlock()
		return client.session == nil && !client.connected && !client.reconnecting
	}, 3*time.Second, time.Millisecond)
}

func TestConnectContextCancellationAndReuse(t *testing.T) {
	url, peers := localWebSocket(t)
	client := New(WithPingInterval(0))
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, client.Connect(ctx, url))
	receivePeer(t, peers)
	client.mu.RLock()
	oldSession, oldConn := client.session, client.conn
	client.mu.RUnlock()
	cancel()
	awaitSessionEnd(t, client)
	require.ErrorIs(t, client.SendText("cancelled"), ErrNotConnected)
	require.NoError(t, client.Connect(t.Context(), url))
	receivePeer(t, peers)
	// A delayed exit from an old read loop must not clear the replacement socket.
	client.onConnectionLost(oldSession, oldConn)
	require.True(t, client.IsConnected())
	messages := make(chan Message, 1)
	client.OnEvent(EventMessage, func(e Event) { messages <- e.Data.(Message) })
	require.NoError(t, client.SendText("new-session"))
	select {
	case msg := <-messages:
		require.Equal(t, "new-session", string(msg.Data))
	case <-time.After(3 * time.Second):
		t.Fatal("replacement connection did not echo")
	}
}

func TestCancelDuringReconnect(t *testing.T) {
	url, peers := localWebSocket(t)
	client := New(WithPingInterval(0), WithReconnect(ReconnectConfig{
		Enable:            true,
		Interval:          time.Hour,
		MaxInterval:       time.Hour,
		BackoffMultiplier: 2,
	}))
	t.Cleanup(func() { _ = client.Close() })
	reconnecting := make(chan struct{}, 1)
	client.OnEvent(EventReconnecting, func(Event) {
		select {
		case reconnecting <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, client.Connect(ctx, url))
	peer := receivePeer(t, peers)
	require.NoError(t, peer.Close())
	select {
	case <-reconnecting:
	case <-time.After(3 * time.Second):
		t.Fatal("did not enter reconnect wait")
	}
	cancel()
	awaitSessionEnd(t, client)
	require.NoError(t, client.Connect(t.Context(), url))
	receivePeer(t, peers)
	require.True(t, client.IsConnected())
}

func TestCancelPendingHandshake(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(exited)
	}))
	defer server.Close()
	defer close(release)
	client := New()
	defer client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Connect(ctx, "ws"+strings.TrimPrefix(server.URL, "http")) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handshake not started")
	}
	require.ErrorIs(t, client.Connect(t.Context(), "ws://127.0.0.1:1"), ErrAlreadyConnected)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("handshake did not cancel")
	}
	awaitSessionEnd(t, client)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("server connection did not close")
	}
}

func TestDisconnectCancelsReconnectWait(t *testing.T) {
	url, peers := localWebSocket(t)
	client := New(WithPingInterval(0), WithReconnect(ReconnectConfig{Enable: true, Interval: time.Hour}))
	defer client.Close()
	reconnecting := make(chan struct{}, 1)
	client.OnEvent(EventReconnecting, func(Event) {
		select {
		case reconnecting <- struct{}{}:
		default:
		}
	})
	require.NoError(t, client.Connect(t.Context(), url))
	peer := receivePeer(t, peers)
	require.NoError(t, peer.Close())
	select {
	case <-reconnecting:
	case <-time.After(3 * time.Second):
		t.Fatal("did not reconnect")
	}
	require.NoError(t, client.Disconnect())
	awaitSessionEnd(t, client)
	require.NoError(t, client.Connect(t.Context(), url))
	receivePeer(t, peers)
}

func TestAutomaticReconnectRetainsCallerContext(t *testing.T) {
	type contextKey struct{}
	url, peers := localWebSocket(t)
	values := make(chan any, 4)
	dialer := &websocket.Dialer{NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		values <- ctx.Value(contextKey{})
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}}
	client := New(
		WithDialer(dialer),
		WithPingInterval(0),
		WithReconnect(ReconnectConfig{
			Enable:            true,
			MaxRetries:        3,
			Interval:          time.Millisecond,
			MaxInterval:       time.Second,
			BackoffMultiplier: 2,
		}),
	)
	defer client.Close()
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "session-value"))
	defer cancel()
	require.NoError(t, client.Connect(ctx, url))
	first := receivePeer(t, peers)
	require.NoError(t, first.Close())
	receivePeer(t, peers)
	for range 2 {
		select {
		case value := <-values:
			require.Equal(t, "session-value", value)
		case <-time.After(3 * time.Second):
			t.Fatal("missing dial context")
		}
	}
	cancel()
	awaitSessionEnd(t, client)
}

func TestObsoleteDialerIsNotInvoked(t *testing.T) {
	url, peers := localWebSocket(t)
	client := New(WithDialer(&websocket.Dialer{
		NetDial: func(string, string) (net.Conn, error) { panic("obsolete NetDial must not be called") },
	}), WithPingInterval(0))
	defer client.Close()
	require.NoError(t, client.Connect(t.Context(), url))
	receivePeer(t, peers)
}
