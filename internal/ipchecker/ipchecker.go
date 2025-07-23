// Package ipchecker provides utilities for extracting and validating
// client IP addresses from HTTP requests. It supports checking whether
// a given IP falls within a trusted subnet.
package ipchecker

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// IPChecker is responsible for extracting a client's IP address from
// an HTTP request and validating whether it belongs to a trusted subnet.
type IPChecker struct {
	trustedSubnet *net.IPNet
}

// New creates a new IPChecker instance configured with a trusted subnet.
// If the input trustedSubnet is an empty string, the IPChecker will be
// initialized in a disabled state - so the IsTrustedSubnetEmpty will return true
//
// The trustedSubnet must be in CIDR notation (e.g., "192.168.1.0/24").
// Returns an error if the CIDR string cannot be parsed.
func New(trustedSubnet string) (*IPChecker, error) {
	if trustedSubnet == "" {
		return &IPChecker{
			trustedSubnet: nil,
		}, nil
	}
	_, allowedNet, err := net.ParseCIDR(trustedSubnet)
	if err != nil {
		return nil, fmt.Errorf("in internal/ipchecker/ipchecker.go/New(): error while `net.ParseCIDR()` calling: %w", err)
	}
	return &IPChecker{
		trustedSubnet: allowedNet,
	}, nil
}

// Check verifies whether the given IP address belongs to the configured
// trusted subnet. If no trusted subnet is configured, it returns false.
func (checker *IPChecker) Check(clientIP net.IP) bool {
	return checker.trustedSubnet != nil && checker.trustedSubnet.Contains(clientIP)
}

// GetClientIP extracts the client's IP address from an HTTP request,
// checking in order: the "X-Real-IP" header, the "X-Forwarded-For" header,
// and finally the request's RemoteAddr field.
//
// Returns the parsed IP address or an error if extraction fails.
func (checker *IPChecker) GetClientIP(request *http.Request) (net.IP, error) {
	ipStr := request.Header.Get("X-Real-IP")
	ip := net.ParseIP(ipStr)
	if ip != nil {
		return ip, nil
	}
	if xff := request.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		ip := strings.TrimSpace(ips[0])
		return net.ParseIP(ip), nil
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("in internal/ipchecker/ipchecker.go/GetClientIP(): error while `net.SplitHostPort()` calling: %w", err)
	}
	return net.ParseIP(host), nil
}

// IsTrustedSubnetEmpty returns true if the IPChecker was initialized
// without a trusted subnet.
func (checker *IPChecker) IsTrustedSubnetEmpty() bool {
	return checker.trustedSubnet == nil
}
