package wsx

import (
	"context"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

// dialContext also interrupts HTTP upgrade reads on cancellation. Gorilla applies
// deadlines, but cancelling a context without a deadline does not interrupt those reads.
func (c *Client) dialContext(ctx context.Context, url string) (*websocket.Conn, *http.Response, error) {
	dialer := *c.dialer
	original := c.dialer
	var stops []func() bool
	bind := func(conn net.Conn, err error) (net.Conn, error) {
		if err == nil {
			// Use the session context, not Gorilla's temporary handshake context:
			// the latter is cancelled even when a handshake succeeds.
			stops = append(stops, context.AfterFunc(ctx, func() { _ = conn.Close() }))
		}
		return conn, err
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	dialer.NetDialContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		if original.NetDialContext != nil {
			return bind(original.NetDialContext(dialCtx, network, address))
		}
		var networkDialer net.Dialer
		return bind(networkDialer.DialContext(dialCtx, network, address))
	}
	dialer.NetDial = nil
	if original.NetDialTLSContext != nil {
		dialer.NetDialTLSContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
			return bind(original.NetDialTLSContext(dialCtx, network, address))
		}
	}
	return dialer.DialContext(ctx, url, c.headers)
}
