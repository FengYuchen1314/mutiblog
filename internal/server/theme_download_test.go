package server

import (
	"net/netip"
	"testing"
)

func TestThemeDownloadURLPolicy(t *testing.T) {
	for _, rawURL := range []string{"http://example.com/theme.zip", "https://user@example.com/theme.zip", "https://example.com:8443/theme.zip", "https://127.0.0.1/theme.zip", "https://[::1]/theme.zip", "https://example.com/theme.zip#fragment"} {
		if err := validateThemeDownloadURL(rawURL); err == nil {
			t.Errorf("validateThemeDownloadURL(%q) accepted an unsafe URL", rawURL)
		}
	}
	if err := validateThemeDownloadURL("https://github.com/example/theme/releases/download/v1/theme.zip"); err != nil {
		t.Fatalf("public HTTPS URL rejected: %v", err)
	}
}

func TestThemeDownloadRejectsNonPublicAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "::1", "fc00::1", "fe80::1", "2001:db8::1"} {
		if isPublicThemeIP(netip.MustParseAddr(raw)) {
			t.Errorf("isPublicThemeIP(%s) = true", raw)
		}
	}
	if !isPublicThemeIP(netip.MustParseAddr("8.8.8.8")) || !isPublicThemeIP(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("known public addresses were rejected")
	}
}
