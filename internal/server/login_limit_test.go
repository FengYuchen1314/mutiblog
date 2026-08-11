package server

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiterHasHardClientBound(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()
	for index := 0; index < loginClientLimit; index++ {
		limiter.attempts[fmt.Sprintf("client-%d", index)] = loginAttempt{Failures: 1, WindowStarted: now}
	}
	if allowed, _ := limiter.Allow("one-client-too-many"); allowed {
		t.Fatal("high-cardinality client was accepted beyond the hard bound")
	}
	limiter.Failed("one-client-too-many")
	if len(limiter.attempts) != loginClientLimit {
		t.Fatalf("attempts = %d, want %d", len(limiter.attempts), loginClientLimit)
	}
	limiter.attempts["client-0"] = loginAttempt{Failures: 1, WindowStarted: now.Add(-loginWindow)}
	if allowed, _ := limiter.Allow("replacement-client"); !allowed {
		t.Fatal("expired login slot was not reclaimed")
	}
}

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
