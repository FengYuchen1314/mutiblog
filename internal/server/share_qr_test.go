package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestPublicShareQRReturnsLocalPNGAndCachesEncoding(t *testing.T) {
	app, service := testShareQRServer(t, "https://blog.example.com")
	var encodes atomic.Int32
	encode := service.encode
	service.encode = func(target string) ([]byte, error) {
		encodes.Add(1)
		return encode(target)
	}
	target := "https://blog.example.com/en/posts/hello/?from=share#section"
	request := shareQRRequest(target, "blog.example.com", "127.0.0.1:4100", "https")
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("share QR status = %d, body = %s", response.Code, response.Body.String())
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("share QR is not a PNG: %x", response.Body.Bytes()[:min(16, response.Body.Len())])
	}
	if response.Header().Get("Content-Type") != "image/png" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" || response.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatalf("share QR security headers = %#v", response.Header())
	}
	if response.Header().Get("Cache-Control") != "public, max-age=3600" || response.Header().Get("ETag") == "" {
		t.Fatalf("share QR cache headers = %#v", response.Header())
	}
	etag := response.Header().Get("ETag")
	request = shareQRRequest(target, "blog.example.com", "127.0.0.1:4101", "https")
	request.Header.Set("If-None-Match", etag)
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotModified || response.Body.Len() != 0 || encodes.Load() != 1 {
		t.Fatalf("cached share QR = status %d, body %d, encodes %d", response.Code, response.Body.Len(), encodes.Load())
	}
}

func TestPublicShareQRRejectsCrossOriginPreviewAndSpoofedHost(t *testing.T) {
	app, _ := testShareQRServer(t, "https://blog.example.com")
	for _, test := range []struct {
		name   string
		target string
		host   string
	}{
		{name: "cross-origin", target: "https://evil.example.net/post", host: "blog.example.com"},
		{name: "preview-host", target: "https://preview.example.com/post", host: "preview.example.com"},
		{name: "host-header-spoof", target: "https://blog.example.com/post", host: "evil.example.net"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := shareQRRequest(test.target, test.host, "127.0.0.1:4100", "https")
			request.Header.Set("X-Forwarded-Host", "blog.example.com")
			response := httptest.NewRecorder()
			app.mux.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("share QR status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPublicShareQRTrustsOnlyFirstForwardedProtoFromTrustedProxy(t *testing.T) {
	app, _ := testShareQRServer(t, "")
	for _, test := range []struct {
		name       string
		remoteAddr string
		forwarded  string
		target     string
		want       int
	}{
		{name: "trusted-first-https", remoteAddr: "127.0.0.1:4100", forwarded: "https, http", target: "https://blog.example.com/post", want: http.StatusOK},
		{name: "trusted-first-http", remoteAddr: "127.0.0.1:4101", forwarded: "http, https", target: "https://blog.example.com/post", want: http.StatusForbidden},
		{name: "untrusted-forwarded-ignored", remoteAddr: "203.0.113.50:4102", forwarded: "https", target: "https://blog.example.com/post", want: http.StatusForbidden},
		{name: "untrusted-direct-http", remoteAddr: "203.0.113.51:4103", forwarded: "https", target: "http://blog.example.com/post", want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := shareQRRequest(test.target, "blog.example.com", test.remoteAddr, test.forwarded)
			response := httptest.NewRecorder()
			app.mux.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("share QR status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestPublicShareQRRejectsLongAndInvalidURLs(t *testing.T) {
	app, _ := testShareQRServer(t, "https://blog.example.com")
	prefix := "https://blog.example.com/"
	maximum := prefix + strings.Repeat("a", shareQRMaxURLBytes-len(prefix))
	request := shareQRRequest(maximum, "blog.example.com", "127.0.0.1:4099", "https")
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.HasPrefix(response.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("maximum-length target status = %d, body prefix = %x", response.Code, response.Body.Bytes()[:min(16, response.Body.Len())])
	}
	for _, target := range []string{
		prefix + strings.Repeat("a", shareQRMaxURLBytes),
		"javascript:alert(1)",
		"https://user@blog.example.com/post",
		"https://blog.example.com/\\\\evil.example.net",
	} {
		request = shareQRRequest(target, "blog.example.com", "127.0.0.1:4100", "https")
		response = httptest.NewRecorder()
		app.mux.ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("target %q status = %d, body = %s", target[:min(len(target), 80)], response.Code, response.Body.String())
		}
	}
}

func TestShareQRLimiterHasHardKeyBoundAndReclaimsExpiredEntries(t *testing.T) {
	limiter := newShareQRLimiter()
	now := time.Unix(1_800_000_000, 0)
	limiter.now = func() time.Time { return now }
	for index := 0; index < shareQRRateKeyLimit; index++ {
		if allowed, _ := limiter.allow(fmt.Sprintf("client-%d", index)); !allowed {
			t.Fatalf("client %d was rejected before hard limit", index)
		}
	}
	if allowed, _ := limiter.allow("overflow"); allowed {
		t.Fatal("overflow client was accepted")
	}
	if len(limiter.entries) != shareQRRateKeyLimit {
		t.Fatalf("limiter entries = %d", len(limiter.entries))
	}
	now = now.Add(shareQRRateWindow + time.Nanosecond)
	if allowed, _ := limiter.allow("reclaimed"); !allowed {
		t.Fatal("expired limiter entries were not reclaimed")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("reclaimed limiter entries = %d", len(limiter.entries))
	}
	for request := 1; request < shareQRRateRequests; request++ {
		if allowed, _ := limiter.allow("reclaimed"); !allowed {
			t.Fatalf("request %d was rejected before per-client limit", request+1)
		}
	}
	if allowed, _ := limiter.allow("reclaimed"); allowed {
		t.Fatal("per-client request overflow was accepted")
	}
}

func TestShareQRCacheIsTTLAndLRUBounded(t *testing.T) {
	service := newShareQRService()
	now := time.Unix(1_800_000_000, 0)
	service.now = func() time.Time { return now }
	var encodes atomic.Int32
	service.encode = func(target string) ([]byte, error) {
		encodes.Add(1)
		return append([]byte("\x89PNG\r\n\x1a\n"), target...), nil
	}
	for index := 0; index < shareQRCacheLimit; index++ {
		if _, err := service.get(context.Background(), fmt.Sprintf("https://blog.example.com/%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.get(context.Background(), "https://blog.example.com/0"); err != nil {
		t.Fatal(err)
	}
	if encodes.Load() != shareQRCacheLimit {
		t.Fatalf("cache hit repeated encoding: %d", encodes.Load())
	}
	if _, err := service.get(context.Background(), "https://blog.example.com/overflow"); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	_, keptRecent := service.cache["https://blog.example.com/0"]
	_, evictedOldest := service.cache["https://blog.example.com/1"]
	cacheSize := len(service.cache)
	service.mu.Unlock()
	if cacheSize != shareQRCacheLimit || !keptRecent || evictedOldest {
		t.Fatalf("LRU cache state = size %d, keptRecent %t, retainedOldest %t", cacheSize, keptRecent, evictedOldest)
	}
	now = now.Add(shareQRCacheTTL + time.Nanosecond)
	if _, err := service.get(context.Background(), "https://blog.example.com/0"); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	cacheSize = len(service.cache)
	service.mu.Unlock()
	if cacheSize != 1 || encodes.Load() != shareQRCacheLimit+2 {
		t.Fatalf("expired cache state = size %d, encodes %d", cacheSize, encodes.Load())
	}
}

func TestShareQRSingleflightAndFlightLimit(t *testing.T) {
	service := newShareQRService()
	started := make(chan struct{}, shareQRFlightLimit)
	release := make(chan struct{})
	var encodes atomic.Int32
	service.encode = func(target string) ([]byte, error) {
		encodes.Add(1)
		started <- struct{}{}
		<-release
		return append([]byte("\x89PNG\r\n\x1a\n"), target...), nil
	}
	results := make(chan error, shareQRFlightLimit+1)
	for index := 0; index < shareQRFlightLimit; index++ {
		target := fmt.Sprintf("https://blog.example.com/%d", index)
		go func() {
			_, err := service.get(context.Background(), target)
			results <- err
		}()
	}
	for index := 0; index < shareQRFlightLimit; index++ {
		<-started
	}
	if _, err := service.get(context.Background(), "https://blog.example.com/overflow"); !errors.Is(err, errShareQRBusy) {
		t.Fatalf("overflow flight error = %v", err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.get(canceledContext, "https://blog.example.com/0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("singleflight waiter cancellation = %v", err)
	}
	join := make(chan error, 1)
	go func() {
		_, err := service.get(context.Background(), "https://blog.example.com/0")
		join <- err
	}()
	close(release)
	for index := 0; index < shareQRFlightLimit; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-join; err != nil {
		t.Fatal(err)
	}
	if encodes.Load() != shareQRFlightLimit {
		t.Fatalf("singleflight encodes = %d", encodes.Load())
	}
}

func shareQRRequest(target, host, remoteAddr, forwardedProto string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/share-qr?url="+url.QueryEscape(target), nil)
	request.Host = host
	request.RemoteAddr = remoteAddr
	if forwardedProto != "" {
		request.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	return request
}

func testShareQRServer(t *testing.T, baseURL string) (*Server, *shareQRService) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "en", BaseURL: baseURL, Locales: map[string]domain.LocalizedSite{"en": {Title: "QR"}}}, false); err != nil {
		t.Fatal(err)
	}
	service := newShareQRService()
	app := &Server{
		repository: repository,
		shareQR:    service,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		mux:        http.NewServeMux(),
	}
	app.routes()
	return app, service
}
