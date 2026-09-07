package netutils

import (
	"net"
	"net/http"
	"strings"
)

type NetworkChecker struct {
	localSubnets []*net.IPNet
}

func NewNetworkChecker(localCIDR string) (*NetworkChecker, error) {
	checker := &NetworkChecker{}
	if localCIDR != "" {
		_, ipNet, err := net.ParseCIDR(localCIDR)
		if err != nil {
			return nil, err
		}
		{
			checker.localSubnets = append(checker.localSubnets, ipNet)
		}
	}
	return checker, nil
}

func GetClientIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	ip := net.ParseIP(remoteHost)
	if ip != nil && (ip.IsLoopback() || remoteHost == "127.0.0.1" || remoteHost == "::1") {
		// Trust X-Real-IP from proxy
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			trimmed := strings.TrimSpace(realIP)
			if parsed := net.ParseIP(trimmed); parsed != nil {
				return trimmed
			}
		}

	}

	return remoteHost
}

func (c *NetworkChecker) IsLocal(r *http.Request) bool {
	clientIPStr := GetClientIP(r)
	ip := net.ParseIP(clientIPStr)
	if ip == nil {
		return false
	}

	if ip.IsLoopback() {
		return true
	}

	for _, subnet := range c.localSubnets {
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}

func IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https"
}
