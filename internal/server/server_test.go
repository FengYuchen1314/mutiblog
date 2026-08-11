package server

import (
	"bytes"
	"context"
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
			{Code: "zh-CN", Enabled: false},
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

func TestSetupLoginSessionAndLogout(t *testing.T) {
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

	assertJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/setup/status", nil, http.StatusOK, map[string]any{"initialized": false})
	setup := map[string]string{
		"siteTitle": "MutiBlog Test", "sourceLocale": "zh-cn", "adminLocale": "en",
		"timezone": "Asia/Shanghai", "username": "admin", "password": "correct horse battery staple",
	}
	response := requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update locales status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var updatedLocales domain.LocalesConfig
	if err := json.NewDecoder(response.Body).Decode(&updatedLocales); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if updatedLocales.SourceLocale != "zh-CN" || len(updatedLocales.Enabled) != 3 || updatedLocales.Enabled[0].Code != "zh-CN" {
		t.Fatalf("updated locales = %#v", updatedLocales)
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

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/admin/posts/hello-world/publish", map[string]int{"revision": 1}, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("publish post status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

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
	setup := map[string]string{"siteTitle": "Security Test", "sourceLocale": "en", "adminLocale": "en", "timezone": "UTC", "username": "admin", "password": oldPassword}
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
