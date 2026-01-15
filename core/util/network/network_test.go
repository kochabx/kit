package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIPv4(t *testing.T) {
	ip := IPv4()
	// IP might be empty if no network connection
	if ip != "" {
		assert.NotEmpty(t, ip)
		// Basic validation: should contain dots
		assert.Contains(t, ip, ".")
	}
}

func TestHostname(t *testing.T) {
	hostname := Hostname()
	assert.NotEmpty(t, hostname)
}

func TestLocalIP(t *testing.T) {
	ip := LocalIP()
	// Test alias works the same as IPv4
	assert.Equal(t, IPv4(), ip)
}
