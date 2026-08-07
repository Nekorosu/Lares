package netutils

import (
	"net/http"
	"testing"
)

func TestNetworkChecker_IsLocal(t *testing.T) {
	checker, err := NewNetworkChecker("192.168.32.0/24")
	if err != nil {
		t.Fatalf("Failed to create network checker: %v", err)
	}

	// Local subnet request via proxy
	reqLocal, _ := http.NewRequest("GET", "/", nil)
	reqLocal.RemoteAddr = "127.0.0.1:12345"
	reqLocal.Header.Set("X-Real-IP", "192.168.32.45")

	if !checker.IsLocal(reqLocal) {
		t.Fatal("Expected 192.168.32.45 to be classified as local")
	}

	// External request via proxy
	reqExt, _ := http.NewRequest("GET", "/", nil)
	reqExt.RemoteAddr = "127.0.0.1:12345"
	reqExt.Header.Set("X-Real-IP", "203.0.113.195")

	if checker.IsLocal(reqExt) {
		t.Fatal("Expected 203.0.113.195 to be classified as external")
	}

	// Direct untrusted connection claiming X-Real-IP
	reqSpoofed, _ := http.NewRequest("GET", "/", nil)
	reqSpoofed.RemoteAddr = "198.51.100.10:54321"
	reqSpoofed.Header.Set("X-Real-IP", "192.168.32.10")

	if checker.IsLocal(reqSpoofed) {
		t.Fatal("Expected spoofed X-Real-IP from external IP to be ignored")
	}
}
