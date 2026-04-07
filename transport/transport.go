package transport

import (
	"context"
	"net"
	"strconv"
)

const (
	MinPort = 1
	MaxPort = 65535
)

// Server defines the interface for transport servers.
// Implementations satisfy cx.Starter and cx.Stopper automatically.
type Server interface {
	// Start starts the server in the background and returns immediately.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the server.
	Stop(ctx context.Context) error
}

// ValidAddress checks whether addr is a syntactically valid host:port pair
// with a port in [1, 65535]. Host validity is left to net.Listen at runtime.
func ValidAddress(addr string) bool {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil || portStr == "" {
		return false
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}
	return p >= MinPort && p <= MaxPort
}
