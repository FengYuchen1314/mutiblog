package server

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPOnlyTrustsForwardingHeadersFromPrivateProxy(t *testing.T) {
	direct := httptest.NewRequest("GET", "https://example.com/", nil)
	direct.RemoteAddr = "198.51.100.8:49152"
	direct.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(direct); got != "198.51.100.8" {
		t.Fatalf("direct client IP = %q", got)
	}

	proxied := httptest.NewRequest("GET", "https://example.com/", nil)
	proxied.RemoteAddr = "127.0.0.1:49152"
	proxied.Header.Set("X-Forwarded-For", "192.0.2.55, 203.0.113.10")
	if got := clientIP(proxied); got != "203.0.113.10" {
		t.Fatalf("proxied client IP = %q", got)
	}

	privateChain := httptest.NewRequest("GET", "https://example.com/", nil)
	privateChain.RemoteAddr = "172.18.0.2:49152"
	privateChain.Header.Set("X-Forwarded-For", "198.51.100.20, 10.0.0.3")
	if got := clientIP(privateChain); got != "198.51.100.20" {
		t.Fatalf("private proxy chain client IP = %q", got)
	}
}
