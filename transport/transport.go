package transport

import (
	"context"
	"net"
	"strconv"
)

const (
	// MinPort defines the minimum valid port number
	MinPort = 1
	// MaxPort defines the maximum valid port number
	MaxPort = 65535
)

// Server defines the interface for transport servers
type Server interface {
	// Run starts the server and blocks until it stops
	Run() error
	// Shutdown gracefully shuts down the server
	Shutdown(context.Context) error
}

// ValidateAddress validates if the given address is a valid network address
func ValidateAddress(addr string) bool {
	// Early return for empty address
	if addr == "" {
		return false
	}

	// Split host and port
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}

	// Early return for empty port
	if port == "" {
		return false
	}

	// Validate host if not empty
	if host != "" && !isValidHost(host) {
		return false
	}

	// Convert port to integer
	p, err := strconv.Atoi(port)
	if err != nil {
		return false
	}

	// Validate port range
	return p >= MinPort && p <= MaxPort
}

// isValidHost validates if the given host is a valid hostname or IP address
func isValidHost(host string) bool {
	// Try to parse as IP address first
	if ip := net.ParseIP(host); ip != nil {
		return true
	}

	// Validate as hostname (basic validation)
	// Empty host is handled in ValidateAddress
	if len(host) == 0 || len(host) > 253 {
		return false
	}

	// Check for valid hostname characters and structure
	// This is a simplified check - for production use, consider more comprehensive validation
	for i, r := range host {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-') {
			return false
		}
		// Hostname cannot start or end with hyphen
		if (i == 0 || i == len(host)-1) && r == '-' {
			return false
		}
	}

	return true
}
