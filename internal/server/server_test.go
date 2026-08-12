package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type fakeSitePublisher struct{}

func (fakeSitePublisher) Build(context.Context) (publisher.BuildReport, error) {
	return publisher.BuildReport{SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

type countingSitePublisher struct{ builds atomic.Int32 }

func (counter *countingSitePublisher) Build(context.Context) (publisher.BuildReport, error) {
	counter.builds.Add(1)
	return publisher.BuildReport{SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

type cancelAwareSitePublisher struct {
	started chan struct{}
	stopped chan struct{}
}

func TestSecureRequestTrustsOnlyTLSOrTrustedProxyFirstHop(t *testing.T) {
	for _, test := range []struct {
		name       string
		remoteAddr string
		forwarded  string
		tls        bool
		want       bool
	}{
		{name: "direct-tls", remoteAddr: "203.0.113.10:443", tls: true, want: true},
		{name: "trusted-proxy-https", remoteAddr: "127.0.0.1:8080", forwarded: "https, http", want: true},
		{name: "trusted-proxy-http", remoteAddr: "127.0.0.1:8080", forwarded: "http, https", want: false},
		{name: "untrusted-spoof", remoteAddr: "203.0.113.10:8080", forwarded: "https", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-Proto", test.forwarded)
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			if got := isSecureRequest(request); got != test.want {
				t.Fatalf("isSecureRequest() = %t, want %t", got, test.want)
			}
		})
	}
}

func (sitePublisher *cancelAwareSitePublisher) Build(ctx context.Context) (publisher.BuildReport, error) {
	close(sitePublisher.started)
	<-ctx.Done()
	close(sitePublisher.stopped)
	return publisher.BuildReport{}, ctx.Err()
}

func TestStartupMigratesLegacyLocaleFallback(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled:       []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}},
		Fallback:      []string{"zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", legacy, false); err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	app.localeProvisioner = localeProvisionerFunc(successfulLocaleProvisioner)
	t.Cleanup(func() { _ = app.Close() })
	var migrated domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &migrated); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Fallback) != 1 || migrated.Fallback[0] != "zh-CN" || migrated.SourceLocale != "zh-CN" || !definitionsEnabled(migrated.Enabled, "en") || !definitionsEnabled(migrated.Enabled, "zh-CN") {
		t.Fatalf("startup-migrated locales = %#v", migrated)
	}
	for _, definition := range migrated.Enabled {
		if definition.Code == "zh-CN" && definition.Status != domain.LocaleStatusReady {
			t.Fatalf("startup-migrated source status = %#v", definition)
		}
		if definition.Code == "en" && definition.Status != "" {
			t.Fatalf("startup-migrated legacy target status = %#v", definition)
		}
	}
}

func TestSetupRejectsNonChineseSource(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	response := requestJSON(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/setup", map[string]string{
		"siteTitle": "French source", "baseUrl": testServer.URL, "sourceLocale": "fr", "adminLocale": "en", "timezone": "UTC",
		"username": "admin", "password": "correct horse battery staple",
	}, nil)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	initialized, err := repository.Exists("config/initialized")
	if err != nil {
		t.Fatal(err)
	}
	if initialized {
		t.Fatal("non-Chinese setup unexpectedly initialized the repository")
	}
}

func TestSetupRequiresAnAbsolutePublicBaseURL(t *testing.T) {
	valid := setupRequest{
		SiteTitle: "Public URL", BaseURL: "https://blog.example.com", SourceLocale: "zh-CN", AdminLocale: "zh-CN", Timezone: "Asia/Shanghai",
		Username: "admin", Password: "correct horse battery staple",
	}
	if fields := validateSetup(&valid); fields["baseUrl"] != "" {
		t.Fatalf("valid setup base URL error = %#v", fields)
	}
	valid.BaseURL = ""
	if fields := validateSetup(&valid); fields["baseUrl"] == "" {
		t.Fatalf("missing base URL unexpectedly accepted: %#v", fields)
	}
	valid.BaseURL = "/relative"
	if fields := validateSetup(&valid); fields["baseUrl"] == "" {
		t.Fatalf("relative base URL unexpectedly accepted: %#v", fields)
	}
}

func TestInitializedSiteBuildsStaticReleaseAtStartup(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/initialized", []byte("initialized\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sitePublisher := &countingSitePublisher{}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: sitePublisher})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	deadline := time.Now().Add(time.Second)
	for sitePublisher.builds.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if sitePublisher.builds.Load() != 1 {
		t.Fatalf("startup builds = %d, want 1", sitePublisher.builds.Load())
	}
}

func TestCloseCancelsAndWaitsForStartupBuild(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/initialized", []byte("initialized\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sitePublisher := &cancelAwareSitePublisher{started: make(chan struct{}), stopped: make(chan struct{})}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: sitePublisher})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sitePublisher.started:
	case <-time.After(time.Second):
		t.Fatal("startup build did not start")
	}
	closed := make(chan struct{})
	go func() {
		_ = app.Close()
		close(closed)
	}()
	select {
	case <-sitePublisher.stopped:
	case <-time.After(time.Second):
		t.Fatal("startup build was not canceled")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("server close did not wait for startup build")
	}
}

func TestErrorResponsesExposeStableRequestID(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/status", nil)
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, request)
	var body struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	headerID := recorder.Header().Get("X-Request-ID")
	decoded, decodeErr := hex.DecodeString(headerID)
	if recorder.Code != http.StatusUnauthorized || body.RequestID != headerID || decodeErr != nil || len(decoded) != 12 {
		t.Fatalf("request ID response = status %d, header %q, body %q, decode %v", recorder.Code, headerID, body.RequestID, decodeErr)
	}
}

func TestPublicReleasePointerCanBeCapturedAndRolledBack(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(repository.Root(), "generated")
	for _, id := range []string{"old", "candidate"} {
		if err := os.MkdirAll(filepath.Join(generated, "releases", id), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(generated, "current")
	if err := os.Symlink(filepath.Join("releases", "old"), current); err != nil {
		t.Fatal(err)
	}
	app := &Server{repository: repository}
	previous, err := app.capturePublicRelease()
	if err != nil || previous != "releases/old" {
		t.Fatalf("captured pointer = %q, %v", previous, err)
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "candidate"), current); err != nil {
		t.Fatal(err)
	}
	if err := app.restorePublicRelease(previous); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(current); err != nil || target != "releases/old" {
		t.Fatalf("restored pointer = %q, %v", target, err)
	}
	if validPublicReleaseTarget("../outside") || validPublicReleaseTarget("/absolute") {
		t.Fatal("unsafe public release target was accepted")
	}
}

func TestRootLocaleNegotiationUsesOnlyTheActiveRelease(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "fr",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Enabled: true},
			{Code: "fr", Enabled: true},
		},
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(repository.Root(), "generated", "current")
	for _, locale := range []string{"zh-CN", "en"} {
		if err := repository.WriteFile(filepath.Join("generated", "current", locale, "index.html"), []byte(locale), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.WriteFile("generated/current/zh-CN/404.html", []byte("published-source-not-found"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/build-report.json", []byte(`{"schemaVersion":1,"locales":["zh-CN","en"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/redirects.json", []byte(`[{"from":"/","to":"/zh-CN/","status":302}]`), 0o640); err != nil {
		t.Fatal(err)
	}
	app := &Server{repository: repository}
	for header, want := range map[string]string{
		"en-GB,en;q=0.9": "/en/",
		"fr,ja;q=0.8":    "/zh-CN/",
		"":               "/zh-CN/",
	} {
		got, ok := app.negotiatedRootTarget(current, header)
		if !ok || got != want {
			t.Fatalf("negotiatedRootTarget(%q) = %q, %v; want %q", header, got, ok, want)
		}
	}
	if got := app.localizedNotFound(current, "missing/resource"); got != filepath.Join(current, "zh-CN", "404.html") {
		t.Fatalf("localizedNotFound() = %q; want active release source 404", got)
	}
}

func TestPublicStaticRoutesHideExplicitNonReadyReleaseLocales(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusFailed},
			{Code: "en", Label: "English", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", config, false); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"generated/current/zh-CN/index.html":      "chinese-home",
		"generated/current/zh-CN/404.html":        "chinese-not-found",
		"generated/current/ja/index.html":         "stale-japanese-home",
		"generated/current/ja/archive/index.html": "stale-japanese-archive",
		"generated/current/ja/404.html":           "stale-japanese-not-found",
		"generated/current/en/index.html":         "legacy-english-home",
	} {
		if err := repository.WriteFile(path, []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.WriteFile("generated/current/build-report.json", []byte(`{"schemaVersion":1,"locales":["zh-CN","ja","en"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/redirects.json", []byte(`[{"from":"/","to":"/zh-CN/","status":302}]`), 0o640); err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(repository.Root(), "generated", "current")
	app := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	request := httptest.NewRequest(http.MethodGet, "/ja/archive/", nil)
	recorder := httptest.NewRecorder()
	app.serveStaticRoot(recorder, request, current)
	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "stale-japanese") {
		t.Fatalf("failed locale direct response = %d %q", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Language", "ja,en;q=0.5")
	recorder = httptest.NewRecorder()
	app.serveStaticRoot(recorder, request, current)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/en/" {
		t.Fatalf("failed locale negotiation = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "/en/", nil)
	recorder = httptest.NewRecorder()
	app.serveStaticRoot(recorder, request, current)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "legacy-english-home" {
		t.Fatalf("statusless legacy response = %d %q", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/ja/archive/", nil)
	recorder = httptest.NewRecorder()
	app.serveStaticRootWithLocaleVisibility(recorder, request, current, false)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "stale-japanese-archive" {
		t.Fatalf("preview response = %d %q", recorder.Code, recorder.Body.String())
	}

	config.Enabled[2].Status = domain.LocaleStatusProvisioning
	if err := repository.WriteYAML("config/locales.yaml", config, false); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/en/", nil)
	recorder = httptest.NewRecorder()
	app.serveStaticRoot(recorder, request, current)
	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "legacy-english") {
		t.Fatalf("retrying legacy response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestLocalizedNotFoundPrefersChineseBeforeEnglishReleaseSource(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/zh-CN/404.html", []byte("chinese-not-found"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/en/404.html", []byte("english-not-found"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/build-report.json", []byte(`{"schemaVersion":1,"locales":["zh-CN","en"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/redirects.json", []byte(`[{"from":"/","to":"/en/","status":302}]`), 0o640); err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(repository.Root(), "generated", "current")
	app := &Server{repository: repository}
	if got := app.localizedNotFound(current, "ja/missing-page"); got != filepath.Join(current, "zh-CN", "404.html") {
		t.Fatalf("localizedNotFound() = %q; want Chinese fallback before English release source", got)
	}
}

func TestSetupLoginSessionAndLogout(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	app.localeProvisioner = localeProvisionerFunc(successfulLocaleProvisioner)
	t.Cleanup(func() { _ = app.Close() })
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	assertJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/setup/status", nil, http.StatusOK, map[string]any{"initialized": false})
	setup := map[string]string{
		"siteTitle": "MutiBlog Test", "baseUrl": testServer.URL, "sourceLocale": "zh-cn", "adminLocale": "en",
		"timezone": "Asia/Shanghai", "username": "admin", "password": "correct horse battery staple",
	}
	response := requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response, err = client.Get(testServer.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("setup session status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	var initialLocales domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &initialLocales); err != nil {
		t.Fatal(err)
	}
	if initialLocales.SourceLocale != "zh-CN" || len(initialLocales.Fallback) != 1 || initialLocales.Fallback[0] != "zh-CN" || len(initialLocales.Enabled) != 1 || initialLocales.Enabled[0].Label != "简体中文" || initialLocales.Enabled[0].Status != domain.LocaleStatusReady || definitionsEnabled(initialLocales.Enabled, "en") {
		t.Fatalf("initial locales = %#v", initialLocales)
	}
	var initialProviders domain.AIProvidersConfig
	if err := repository.ReadYAML("config/providers.yaml", &initialProviders); err != nil {
		t.Fatal(err)
	}
	if initialProviders.DefaultProvider != "google-free" || len(initialProviders.Providers) != 1 {
		t.Fatalf("initial providers = %#v", initialProviders)
	}
	google := initialProviders.Providers[0]
	expectedGoogle := domain.AIProviderConfig{
		ID:              "google-free",
		Name:            "Google Free Translate",
		Kind:            "google-free",
		BaseURL:         "https://translate.googleapis.com/translate_a/single",
		Model:           "google-translate",
		Enabled:         true,
		TimeoutSeconds:  45,
		MaxOutputTokens: 8192,
	}
	if google != expectedGoogle {
		t.Fatalf("initial Google provider = %#v", google)
	}
	var initialSecrets domain.SecretsConfig
	if err := repository.ReadYAML("config/secrets.yaml", &initialSecrets); err != nil {
		t.Fatal(err)
	}
	if len(initialSecrets.Providers) != 0 {
		t.Fatalf("setup unexpectedly stored provider secrets = %#v", initialSecrets.Providers)
	}
	if err := repository.WriteFile("generated/current/redirects.json", []byte(`[{"from":"/ja/posts/hello-world/","to":"/zh-CN/posts/hello-world/","status":302}]`), 0o640); err != nil {
		t.Fatal(err)
	}
	redirectClient := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err = redirectClient.Get(testServer.URL + "/ja/posts/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusFound || response.Header.Get("Location") != "/zh-CN/posts/hello-world/" {
		t.Fatalf("fallback redirect = %d %q", response.StatusCode, response.Header.Get("Location"))
	}
	response.Body.Close()
	if err := repository.WriteFile("generated/current/en/index.html", []byte("static-public-page"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/current/zh-CN/404.html", []byte("themed-not-found"), 0o640); err != nil {
		t.Fatal(err)
	}
	notFoundResponse, err := client.Get(testServer.URL + "/missing/resource")
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, notFoundResponse); notFoundResponse.StatusCode != http.StatusNotFound || body != "themed-not-found" {
		t.Fatalf("localized 404 = %d %q", notFoundResponse.StatusCode, body)
	}
	notFoundResponse.Body.Close()
	response, err = client.Get(testServer.URL + "/en/")
	if err != nil {
		t.Fatal(err)
	}
	etag := response.Header.Get("ETag")
	if response.StatusCode != http.StatusOK || etag == "" || response.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("static response = %d, etag %q, cache %q", response.StatusCode, etag, response.Header.Get("Cache-Control"))
	}
	response.Body.Close()
	conditional, err := http.NewRequest(http.MethodGet, testServer.URL+"/en/", nil)
	if err != nil {
		t.Fatal(err)
	}
	conditional.Header.Set("If-None-Match", etag)
	response, err = client.Do(conditional)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional static response = %d", response.StatusCode)
	}
	response.Body.Close()
	if info, err := os.Stat(filepath.Join(repository.Root(), "config", "admin.yaml")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("admin config mode = %v, err = %v", info.Mode().Perm(), err)
	}

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("second setup status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong password"}, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid login status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": setup["password"]}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var session map[string]any
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	csrf := session["csrfToken"].(string)
	response = requestJSON(t, client, http.MethodPut, testServer.URL+"/api/v1/admin/locales", map[string]any{"enabled": []map[string]any{
		{"code": "zh-cn", "label": "简体中文", "enabled": true},
		{"code": "en", "label": "English", "enabled": true},
		{"code": "ja", "label": "日本語", "enabled": true},
	}}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("update locales status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var updateLocalesResult struct {
		Locales domain.LocalesConfig `json:"locales"`
		Task    adminTask            `json:"task"`
		Build   struct {
			Status string `json:"status"`
		} `json:"build"`
	}
	if err := json.NewDecoder(response.Body).Decode(&updateLocalesResult); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	updatedLocales := updateLocalesResult.Locales
	if updateLocalesResult.Build.Status != "deferred" || updateLocalesResult.Task.Kind != "LocaleProvision" {
		t.Fatalf("update locales build = %#v", updateLocalesResult.Build)
	}
	if updatedLocales.SourceLocale != "zh-CN" || len(updatedLocales.Enabled) != 3 || updatedLocales.Enabled[0].Code != "zh-CN" || len(updatedLocales.Fallback) != 1 || updatedLocales.Fallback[0] != "zh-CN" {
		t.Fatalf("updated locales = %#v", updatedLocales)
	}
	if updatedLocales.Enabled[0].Status != domain.LocaleStatusReady || updatedLocales.Enabled[1].Status != domain.LocaleStatusProvisioning || updatedLocales.Enabled[2].Status != domain.LocaleStatusProvisioning {
		t.Fatalf("updated locale statuses = %#v", updatedLocales.Enabled)
	}
	localeTask := waitLocaleProvisionTask(t, app.localeTasks, updateLocalesResult.Task.ID)
	if localeTask.Status != "succeeded" || localeTask.BuildStatus != "succeeded" {
		t.Fatalf("locale provisioning task = %#v", localeTask)
	}
	updatedLocales = readLocaleTestConfig(t, repository)
	if updatedLocales.Enabled[1].Status != domain.LocaleStatusReady || updatedLocales.Enabled[2].Status != domain.LocaleStatusReady {
		t.Fatalf("completed locale statuses = %#v", updatedLocales.Enabled)
	}
	const providerSecret = "server-test-provider-secret"
	response = requestJSON(t, client, http.MethodPut, testServer.URL+"/api/v1/admin/ai/providers/test-provider", map[string]any{
		"name": "Test Provider", "kind": "openai-compatible", "baseUrl": "https://api.example.invalid/v1", "model": "test-model",
		"enabled": false, "default": true, "timeoutSeconds": 45, "maxOutputTokens": 8192, "apiKey": providerSecret,
	}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save provider status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	providerBody := readBody(t, response)
	if strings.Contains(providerBody, providerSecret) || !strings.Contains(providerBody, `"maskedKey":"••••cret"`) {
		t.Fatalf("unsafe provider response = %s", providerBody)
	}
	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/admin/ai/providers", nil, nil)
	listedProviders := readBody(t, response)
	if strings.Contains(listedProviders, providerSecret) || !strings.Contains(listedProviders, "test-provider") {
		t.Fatalf("unsafe provider list = %s", listedProviders)
	}

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/admin/posts", map[string]string{"id": "hello-world", "title": "Hello"}, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("create post without csrf status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/admin/posts", map[string]string{"id": "hello-world", "title": "Hello", "seoTitle": "Search title", "seoDescription": "Search description", "markdown": "# Hello"}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create post status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var createdPost struct {
		Meta struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
		} `json:"meta"`
		Content map[string]struct {
			SEOTitle       string `json:"seoTitle"`
			SEODescription string `json:"seoDescription"`
		} `json:"content"`
	}
	if err := json.NewDecoder(response.Body).Decode(&createdPost); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if createdPost.Meta.ID != "hello-world" || createdPost.Meta.Revision != 1 {
		t.Fatalf("created post = %#v", createdPost)
	}
	if createdPost.Content["zh-CN"].SEOTitle != "Search title" || createdPost.Content["zh-CN"].SEODescription != "Search description" {
		t.Fatalf("created post SEO = %#v", createdPost.Content)
	}
	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/admin/posts", map[string]string{"id": "hello-world", "title": "Duplicate"}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate post status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var duplicateError struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&duplicateError); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if duplicateError.Code != "post_id_exists" {
		t.Fatalf("duplicate post code = %q", duplicateError.Code)
	}

	settingsBody := map[string]any{
		"revision": 1, "categories": []string{}, "tags": []string{}, "cover": "", "pinned": true,
		"visibility": "public", "publishedAt": "not-a-time", "commentPolicy": "open", "template": "post",
	}
	response = requestJSON(t, client, http.MethodPut, testServer.URL+"/api/v1/admin/posts/hello-world", settingsBody, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid post settings status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	settingsBody["publishedAt"] = "2026-08-10T08:30:00Z"
	response = requestJSON(t, client, http.MethodPut, testServer.URL+"/api/v1/admin/posts/hello-world", settingsBody, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("post settings status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var configuredPost domain.Post
	if err := json.NewDecoder(response.Body).Decode(&configuredPost); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if configuredPost.Meta.Revision != 2 || !configuredPost.Meta.Pinned || configuredPost.Meta.Visibility != domain.ContentVisibilityPublic || configuredPost.Meta.PublishedAt == nil || configuredPost.Meta.PublishedAt.Format(time.RFC3339) != "2026-08-10T08:30:00Z" {
		t.Fatalf("configured post = %#v", configuredPost.Meta)
	}

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/admin/posts/hello-world/publish", map[string]int{"revision": configuredPost.Meta.Revision}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("publish post status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var publishResult struct {
		Build struct {
			Status string `json:"status"`
		} `json:"build"`
		Translation struct {
			Status string `json:"status"`
			TaskID string `json:"taskId"`
		} `json:"translation"`
	}
	if err := json.NewDecoder(response.Body).Decode(&publishResult); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Header.Get("X-MutiBlog-Static-Build") != "blocked" || publishResult.Build.Status != "blocked" || publishResult.Translation.Status != "not-configured" || publishResult.Translation.TaskID == "" {
		t.Fatalf("publish post result = %#v, build header = %q", publishResult, response.Header.Get("X-MutiBlog-Static-Build"))
	}

	var imageData bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{G: 255, A: 255})
	if err := png.Encode(&imageData, canvas); err != nil {
		t.Fatal(err)
	}
	response = requestMultipart(t, client, testServer.URL+"/api/v1/admin/attachments", "test.png", imageData.Bytes(), nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("media upload without csrf status = %d", response.StatusCode)
	}
	response.Body.Close()
	response = requestMultipart(t, client, testServer.URL+"/api/v1/admin/attachments", "test.png", imageData.Bytes(), map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("media upload status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var asset struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&asset); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	response, err = client.Get(testServer.URL + asset.URL)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("public media status = %d, type = %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/auth/session", nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/logout", nil, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without csrf status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/logout", nil, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/auth/session", nil, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestChangePasswordInvalidatesSessionsAndOldCredential(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	oldPassword := "correct horse battery staple"
	newPassword := "another correct horse battery staple"
	setup := map[string]string{"siteTitle": "Security Test", "baseUrl": testServer.URL, "sourceLocale": "zh-CN", "adminLocale": "en", "timezone": "UTC", "username": "admin", "password": oldPassword}
	response := requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": oldPassword}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var session map[string]any
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	csrf := session["csrfToken"].(string)
	response = requestJSON(t, client, http.MethodPut, testServer.URL+"/api/v1/admin/security/password", map[string]string{"currentPassword": "incorrect password", "newPassword": newPassword}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(readBody(t, response), `"code":"current_password_invalid"`) {
		t.Fatalf("wrong current password response = %d", response.StatusCode)
	}
	response = requestJSON(t, client, http.MethodPut, testServer.URL+"/api/v1/admin/security/password", map[string]string{"currentPassword": oldPassword, "newPassword": newPassword}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("change password status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/auth/session", nil, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old session status = %d", response.StatusCode)
	}
	response.Body.Close()
	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": oldPassword}, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old credential status = %d", response.StatusCode)
	}
	response.Body.Close()
	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": newPassword}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("new credential status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/admin/security/audit?limit=20", nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("audit status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	auditBody := readBody(t, response)
	for _, expected := range []string{`"action":"setup"`, `"action":"login"`, `"action":"password-change"`, `"outcome":"succeeded"`} {
		if !strings.Contains(auditBody, expected) {
			t.Fatalf("audit body missing %s: %s", expected, auditBody)
		}
	}
}

func requestMultipart(t *testing.T, client *http.Client, url, filename string, data []byte, headers map[string]string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertJSON(t *testing.T, client *http.Client, method, url string, body any, status int, want map[string]any) {
	t.Helper()
	response := requestJSON(t, client, method, url, body, nil)
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("status = %d, want %d", response.StatusCode, status)
	}
	var got map[string]any
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %v, want %v", key, got[key], value)
		}
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return string(data)
}
