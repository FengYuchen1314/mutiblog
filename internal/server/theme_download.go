package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/themes"
)

type installThemeURLRequest struct {
	URL string `json:"url"`
}

func (s *Server) handleInstallThemeURL(w http.ResponseWriter, r *http.Request) {
	var request installThemeURLRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	archive, err := downloadThemeArchive(r.Context(), request.URL)
	if err != nil {
		s.logger.Warn("remote theme download rejected")
		s.writeError(w, http.StatusUnprocessableEntity, "theme_download_failed", "The remote theme could not be downloaded safely.", nil)
		return
	}
	s.installThemeReader(w, r, bytes.NewReader(archive))
}

func downloadThemeArchive(ctx context.Context, rawURL string) ([]byte, error) {
	response, err := openPublicHTTPSArchive(ctx, rawURL, themes.MaxArchiveSize, "application/zip, application/octet-stream;q=0.9", "MutiBlog-Theme-Installer/1", 45*time.Second)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, themes.MaxArchiveSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > themes.MaxArchiveSize {
		return nil, errors.New("remote theme has an invalid size")
	}
	return data, nil
}

func openPublicHTTPSArchive(ctx context.Context, rawURL string, maxBytes int64, accept, userAgent string, timeout time.Duration) (*http.Response, error) {
	if err := validateThemeDownloadURL(rawURL); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 20 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				if isPublicThemeIP(address) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
				}
			}
			return nil, errors.New("remote host has no public IP address")
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many remote archive redirects")
			}
			return validateThemeDownloadURL(request.URL.String())
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("remote archive returned %s", response.Status)
	}
	if response.ContentLength > maxBytes {
		response.Body.Close()
		return nil, errors.New("remote archive exceeds the size limit")
	}
	return response, nil
}

func validateThemeDownloadURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("theme URL must be a plain HTTPS URL")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return errors.New("theme URL must use HTTPS port 443")
	}
	if address, err := netip.ParseAddr(parsed.Hostname()); err == nil && !isPublicThemeIP(address) {
		return errors.New("theme URL cannot target a private address")
	}
	return nil
}

func isPublicThemeIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range nonPublicThemePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var nonPublicThemePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
}
