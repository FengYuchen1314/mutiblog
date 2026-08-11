package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit = 8
	loginWindow       = 10 * time.Minute
	loginBlock        = 15 * time.Minute
)

type loginAttempt struct {
	Failures      int
	WindowStarted time.Time
	BlockedUntil  time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, ok := l.attempts[key]
	if !ok {
		return true, 0
	}
	if now.Before(attempt.BlockedUntil) {
		return false, time.Until(attempt.BlockedUntil)
	}
	if now.Sub(attempt.WindowStarted) >= loginWindow {
		delete(l.attempts, key)
	}
	return true, 0
}

func (l *loginLimiter) Failed(key string) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if attempt.WindowStarted.IsZero() || now.Sub(attempt.WindowStarted) >= loginWindow {
		attempt = loginAttempt{WindowStarted: now}
	}
	attempt.Failures++
	if attempt.Failures >= loginFailureLimit {
		attempt.BlockedUntil = now.Add(loginBlock)
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	peer := remoteIP(r.RemoteAddr)
	if !trustedProxyIP(peer) {
		return peer.String()
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		chain := strings.Split(forwarded, ",")
		for index := len(chain) - 1; index >= 0; index-- {
			candidate := net.ParseIP(strings.TrimSpace(chain[index]))
			if candidate == nil {
				continue
			}
			if !trustedProxyIP(candidate) {
				return candidate.String()
			}
			peer = candidate
		}
	}
	if realIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != nil {
		return realIP.String()
	}
	return peer.String()
}

func remoteIP(remoteAddress string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		if parsed := net.ParseIP(host); parsed != nil {
			return parsed
		}
	}
	return net.ParseIP(strings.TrimSpace(remoteAddress))
}

func trustedProxyIP(address net.IP) bool {
	return address != nil && (address.IsLoopback() || address.IsPrivate())
}
