package server

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/skip2/go-qrcode"
)

const (
	shareQRMaxURLBytes      = 2048
	shareQRMaxRawQueryBytes = 8192
	shareQRImageSize        = 512
	shareQRCacheLimit       = 256
	shareQRCacheTTL         = 15 * time.Minute
	shareQRFlightLimit      = 32
	shareQRRateKeyLimit     = 4096
	shareQRRateWindow       = time.Minute
	shareQRRateRequests     = 60
)

var (
	errShareQRInvalid           = errors.New("invalid share QR request")
	errShareQRForbidden         = errors.New("share QR origin is forbidden")
	errShareQRPreview           = errors.New("share QR is unavailable on preview hosts")
	errShareQROriginUnavailable = errors.New("share QR public origin is unavailable")
	errShareQRBusy              = errors.New("share QR generator is busy")
)

type shareQRResult struct {
	png  []byte
	etag string
}

type shareQRCacheEntry struct {
	result    shareQRResult
	expiresAt time.Time
	element   *list.Element
}

type shareQRFlight struct {
	done   chan struct{}
	result shareQRResult
	err    error
}

// shareQRService keeps both completed and in-flight work absolutely bounded.
// It intentionally owns no network client: QR generation is a local, pure
// encoding operation and can never become an SSRF path.
type shareQRService struct {
	mu      sync.Mutex
	cache   map[string]*shareQRCacheEntry
	lru     *list.List
	flights map[string]*shareQRFlight
	limiter *shareQRLimiter
	now     func() time.Time
	encode  func(string) ([]byte, error)
}

type shareQRRateEntry struct {
	requests int
	expires  time.Time
}

type shareQRLimiter struct {
	mu      sync.Mutex
	entries map[string]shareQRRateEntry
	now     func() time.Time
}

type shareQROrigin struct {
	scheme    string
	authority string
	hostname  string
}

func newShareQRService() *shareQRService {
	return &shareQRService{
		cache:   make(map[string]*shareQRCacheEntry),
		lru:     list.New(),
		flights: make(map[string]*shareQRFlight),
		limiter: newShareQRLimiter(),
		now:     time.Now,
		encode: func(target string) ([]byte, error) {
			return qrcode.Encode(target, qrcode.Medium, shareQRImageSize)
		},
	}
}

func newShareQRLimiter() *shareQRLimiter {
	return &shareQRLimiter{entries: make(map[string]shareQRRateEntry), now: time.Now}
}

func (l *shareQRLimiter) allow(rawKey string) (bool, time.Duration) {
	key := strings.TrimSpace(rawKey)
	if key == "" {
		key = "unknown"
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.entries[key]; ok {
		if !entry.expires.After(now) {
			l.entries[key] = shareQRRateEntry{requests: 1, expires: now.Add(shareQRRateWindow)}
			return true, 0
		}
		if entry.requests >= shareQRRateRequests {
			return false, entry.expires.Sub(now)
		}
		entry.requests++
		l.entries[key] = entry
		return true, 0
	}
	for candidate, entry := range l.entries {
		if !entry.expires.After(now) {
			delete(l.entries, candidate)
		}
	}
	if len(l.entries) >= shareQRRateKeyLimit {
		return false, shareQRRateWindow
	}
	l.entries[key] = shareQRRateEntry{requests: 1, expires: now.Add(shareQRRateWindow)}
	return true, 0
}

func (s *shareQRService) get(ctx context.Context, target string) (shareQRResult, error) {
	now := s.now()
	s.mu.Lock()
	s.pruneExpiredLocked(now)
	if cached, ok := s.cache[target]; ok {
		s.lru.MoveToFront(cached.element)
		result := cached.result
		s.mu.Unlock()
		return result, nil
	}
	if flight, ok := s.flights[target]; ok {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return shareQRResult{}, ctx.Err()
		case <-flight.done:
			return flight.result, flight.err
		}
	}
	if len(s.flights) >= shareQRFlightLimit {
		s.mu.Unlock()
		return shareQRResult{}, errShareQRBusy
	}
	flight := &shareQRFlight{done: make(chan struct{})}
	s.flights[target] = flight
	s.mu.Unlock()

	png, err := s.encodeSafely(target)
	result := shareQRResult{}
	if err == nil {
		digest := sha256.Sum256(png)
		result = shareQRResult{png: png, etag: `"` + hex.EncodeToString(digest[:]) + `"`}
	}

	s.mu.Lock()
	delete(s.flights, target)
	if err == nil {
		s.insertLocked(target, result, s.now().Add(shareQRCacheTTL))
	}
	flight.result = result
	flight.err = err
	close(flight.done)
	s.mu.Unlock()
	return result, err
}

func (s *shareQRService) encodeSafely(target string) (png []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			png = nil
			err = fmt.Errorf("QR encoder panic: %v", recovered)
		}
	}()
	return s.encode(target)
}

func (s *shareQRService) pruneExpiredLocked(now time.Time) {
	for target, cached := range s.cache {
		if cached.expiresAt.After(now) {
			continue
		}
		s.lru.Remove(cached.element)
		delete(s.cache, target)
	}
}

func (s *shareQRService) insertLocked(target string, result shareQRResult, expiresAt time.Time) {
	if cached, ok := s.cache[target]; ok {
		cached.result = result
		cached.expiresAt = expiresAt
		s.lru.MoveToFront(cached.element)
		return
	}
	for len(s.cache) >= shareQRCacheLimit {
		oldest := s.lru.Back()
		if oldest == nil {
			break
		}
		delete(s.cache, oldest.Value.(string))
		s.lru.Remove(oldest)
	}
	element := s.lru.PushFront(target)
	s.cache[target] = &shareQRCacheEntry{result: result, expiresAt: expiresAt, element: element}
}

func (s *Server) handlePublicShareQR(w http.ResponseWriter, r *http.Request) {
	setShareQRResponseSecurityHeaders(w)
	if s.shareQR == nil {
		s.writeError(w, http.StatusServiceUnavailable, "share_qr_unavailable", "The share QR service is unavailable.", nil)
		return
	}
	allowed, retryAfter := s.shareQR.limiter.allow(clientIP(r))
	if !allowed {
		s.writeShareQRError(w, errShareQRBusy, retryAfter)
		return
	}
	origin, err := s.resolveShareQROrigin(r)
	if err != nil {
		s.writeShareQRError(w, err, 0)
		return
	}
	target, err := parseShareQRTarget(r, origin)
	if err != nil {
		s.writeShareQRError(w, err, 0)
		return
	}
	result, err := s.shareQR.get(r.Context(), target)
	if err != nil {
		s.writeShareQRError(w, err, time.Second)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", result.etag)
	if etagMatches(r.Header.Get("If-None-Match"), result.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(result.png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.png)
}

func setShareQRResponseSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Server) writeShareQRError(w http.ResponseWriter, err error, retryAfter time.Duration) {
	switch {
	case errors.Is(err, errShareQRInvalid):
		s.writeError(w, http.StatusUnprocessableEntity, "share_qr_invalid", "The share URL is invalid.", nil)
	case errors.Is(err, errShareQRForbidden), errors.Is(err, errShareQRPreview):
		s.writeError(w, http.StatusForbidden, "share_qr_forbidden", "Only same-origin public URLs can be encoded.", nil)
	case errors.Is(err, errShareQRBusy):
		seconds := int((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		s.writeError(w, http.StatusTooManyRequests, "share_qr_rate_limited", "Too many QR requests were submitted. Please try again later.", nil)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The client is gone; avoid starting a second response after cancellation.
		return
	case errors.Is(err, errShareQROriginUnavailable):
		s.writeError(w, http.StatusServiceUnavailable, "share_qr_origin_unavailable", "The public site origin is unavailable.", nil)
	default:
		s.logger.Error("share QR generation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "share_qr_unavailable", "The share QR service is unavailable.", nil)
	}
}

func (s *Server) resolveShareQROrigin(r *http.Request) (shareQROrigin, error) {
	scheme, err := effectiveShareQRScheme(r)
	if err != nil {
		return shareQROrigin{}, errShareQRForbidden
	}
	requestAuthority, requestHostname, err := canonicalShareQRAuthority(r.Host, scheme)
	if err != nil {
		return shareQROrigin{}, errShareQRForbidden
	}
	if isReservedPreviewHostname(requestHostname) {
		return shareQROrigin{}, errShareQRPreview
	}

	var site domain.SiteConfig
	err = s.repository.ReadYAML("config/site.yaml", &site)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return shareQROrigin{}, fmt.Errorf("%w: %v", errShareQROriginUnavailable, err)
	}
	baseURL := strings.TrimSpace(site.BaseURL)
	if baseURL == "" {
		return shareQROrigin{scheme: scheme, authority: requestAuthority, hostname: requestHostname}, nil
	}
	configured, err := parseConfiguredShareQROrigin(baseURL)
	if err != nil {
		return shareQROrigin{}, fmt.Errorf("%w: %v", errShareQROriginUnavailable, err)
	}
	if scheme != configured.scheme {
		return shareQROrigin{}, errShareQRForbidden
	}
	if strings.EqualFold(requestAuthority, previewAuthority(configured)) {
		return shareQROrigin{}, errShareQRPreview
	}
	if !strings.EqualFold(requestAuthority, configured.authority) {
		return shareQROrigin{}, errShareQRForbidden
	}
	return configured, nil
}

func effectiveShareQRScheme(r *http.Request) (string, error) {
	if r.TLS != nil {
		return "https", nil
	}
	if !trustedProxyIP(remoteIP(r.RemoteAddr)) {
		return "http", nil
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
	if len(forwarded) == 0 || strings.TrimSpace(forwarded[0]) == "" {
		return "http", nil
	}
	scheme := strings.ToLower(strings.TrimSpace(forwarded[0]))
	if scheme != "http" && scheme != "https" {
		return "", errShareQRForbidden
	}
	return scheme, nil
}

func parseConfiguredShareQROrigin(raw string) (shareQROrigin, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return shareQROrigin{}, errShareQROriginUnavailable
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return shareQROrigin{}, errShareQROriginUnavailable
	}
	authority, hostname, err := canonicalShareQRAuthority(parsed.Host, scheme)
	if err != nil || isReservedPreviewHostname(hostname) {
		return shareQROrigin{}, errShareQROriginUnavailable
	}
	return shareQROrigin{scheme: scheme, authority: authority, hostname: hostname}, nil
}

func parseShareQRTarget(r *http.Request, origin shareQROrigin) (string, error) {
	if len(r.URL.RawQuery) > shareQRMaxRawQueryBytes {
		return "", errShareQRInvalid
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 1 {
		return "", errShareQRInvalid
	}
	values := query["url"]
	if len(values) != 1 {
		return "", errShareQRInvalid
	}
	rawTarget := values[0]
	if rawTarget == "" || len(rawTarget) > shareQRMaxURLBytes || strings.TrimSpace(rawTarget) != rawTarget || strings.ContainsRune(rawTarget, '\\') {
		return "", errShareQRInvalid
	}
	target := rawTarget
	parsed, err := url.Parse(target)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" {
		return "", errShareQRInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errShareQRInvalid
	}
	authority, _, err := canonicalShareQRAuthority(parsed.Host, scheme)
	if err != nil {
		return "", errShareQRInvalid
	}
	if scheme != origin.scheme || !strings.EqualFold(authority, origin.authority) {
		return "", errShareQRForbidden
	}
	return target, nil
}

func canonicalShareQRAuthority(rawAuthority, scheme string) (string, string, error) {
	if rawAuthority == "" || strings.TrimSpace(rawAuthority) != rawAuthority || strings.ContainsAny(rawAuthority, `/\\?#`) {
		return "", "", errShareQRInvalid
	}
	parsed, err := url.Parse("//" + rawAuthority)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errShareQRInvalid
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return "", "", errShareQRInvalid
	}
	hostname := strings.ToLower(parsed.Hostname())
	if !validShareQRHostname(hostname) || strings.ContainsRune(hostname, '%') {
		return "", "", errShareQRInvalid
	}
	if strings.Contains(hostname, ":") && !strings.HasPrefix(parsed.Host, "[") {
		return "", "", errShareQRInvalid
	}
	port := parsed.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", "", errShareQRInvalid
		}
		if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			port = ""
		}
	}
	authority := hostname
	if net.ParseIP(hostname) != nil && strings.Contains(hostname, ":") {
		authority = "[" + hostname + "]"
	}
	if port != "" {
		authority = net.JoinHostPort(hostname, port)
	}
	return authority, hostname, nil
}

func validShareQRHostname(hostname string) bool {
	if hostname == "" {
		return false
	}
	if net.ParseIP(hostname) != nil {
		return true
	}
	if len(hostname) > 253 || strings.HasSuffix(hostname, ".") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func isReservedPreviewHostname(hostname string) bool {
	if net.ParseIP(hostname) != nil {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(strings.ToLower(hostname), "."), ".")
	return len(parts) > 0 && parts[0] == "preview"
}

func previewAuthority(origin shareQROrigin) string {
	if net.ParseIP(origin.hostname) != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSuffix(origin.hostname, "."), ".")
	if len(parts) >= 3 {
		parts[0] = "preview"
	} else {
		parts = append([]string{"preview"}, parts...)
	}
	hostname := strings.Join(parts, ".")
	_, port, err := net.SplitHostPort(origin.authority)
	if err == nil && port != "" {
		return net.JoinHostPort(hostname, port)
	}
	return hostname
}
