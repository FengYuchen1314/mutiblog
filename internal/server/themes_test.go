package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/audit"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/themes"
)

type failingThemePublisher struct{}

func (failingThemePublisher) Build(context.Context) (publisher.BuildReport, error) {
	return publisher.BuildReport{}, errors.New("renderer failed")
}

type countingThemePublisher struct{ calls int }

func (counter *countingThemePublisher) Build(context.Context) (publisher.BuildReport, error) {
	counter.calls++
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
}

func installThemeForTest(t *testing.T, service *themes.Service, reader io.Reader) (themes.View, error) {
	t.Helper()
	installation, err := service.BeginInstall(reader)
	if err != nil {
		return themes.View{}, err
	}
	defer func() {
		if err := installation.Rollback(); err != nil {
			t.Errorf("rollback theme installation: %v", err)
		}
	}()
	if err := installation.Commit(); err != nil && !errors.Is(err, themes.ErrCleanupPending) {
		return themes.View{}, err
	}
	return installation.View, nil
}

func TestActiveThemeUpgradeRollsBackPackageOnRenderFailure(t *testing.T) {
	server, repository := themeServerFixture(t, failingThemePublisher{})
	if _, err := installThemeForTest(t, server.themes, serverThemeArchive(t, "midnight", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	if err := server.themes.Activate("midnight"); err != nil {
		t.Fatal(err)
	}

	request := themeUploadRequest(t, serverThemeArchive(t, "midnight", "2.0.0"))
	recorder := httptest.NewRecorder()
	server.handleInstallTheme(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "theme_render_failed") {
		t.Fatalf("upgrade response = %d %s", recorder.Code, recorder.Body.String())
	}
	items, err := server.themes.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "midnight" && item.Version != "1.0.0" {
			t.Fatalf("failed upgrade kept version %q", item.Version)
		}
	}
	var site domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	if site.ActiveTheme != "midnight" {
		t.Fatalf("failed upgrade changed active theme to %q", site.ActiveTheme)
	}
}

func TestThemeActivationRollsBackConfigurationOnRenderFailure(t *testing.T) {
	server, repository := themeServerFixture(t, failingThemePublisher{})
	if _, err := installThemeForTest(t, server.themes, serverThemeArchive(t, "midnight", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/midnight/activate", nil)
	request.SetPathValue("id", "midnight")
	recorder := httptest.NewRecorder()
	server.handleActivateTheme(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("activation response = %d %s", recorder.Code, recorder.Body.String())
	}
	var site domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	if site.ActiveTheme != "earth" {
		t.Fatalf("failed activation left active theme %q", site.ActiveTheme)
	}
}

func TestThemeReloadBuildsOnlyTheActiveTheme(t *testing.T) {
	publisher := &countingThemePublisher{}
	server, _ := themeServerFixture(t, publisher)
	if _, err := installThemeForTest(t, server.themes, serverThemeArchive(t, "midnight", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"midnight", "earth"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/"+id+"/reload", nil)
		request.SetPathValue("id", id)
		recorder := httptest.NewRecorder()
		server.handleReloadTheme(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("reload %s = %d %s", id, recorder.Code, recorder.Body.String())
		}
	}
	if publisher.calls != 1 {
		t.Fatalf("active reload builds = %d, want 1", publisher.calls)
	}
}

func TestThemeScreenshotServesInstalledLocalImage(t *testing.T) {
	server, _ := themeServerFixture(t, failingThemePublisher{})
	archive := themeArchiveFiles(t, map[string]string{
		"theme.yaml":   "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\nscreenshot: preview.webp\n",
		"server.mjs":   "export const css = '';\n",
		"preview.webp": "preview-data",
	})
	if _, err := installThemeForTest(t, server.themes, archive); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/themes/midnight/screenshot", nil)
	request.SetPathValue("id", "midnight")
	recorder := httptest.NewRecorder()
	server.handleThemeScreenshot(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/webp" || recorder.Body.String() != "preview-data" {
		t.Fatalf("screenshot response = %d %q %q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestActiveThemeUpgradeRejectsIncompatiblePackage(t *testing.T) {
	server, _ := themeServerFixture(t, failingThemePublisher{})
	if _, err := installThemeForTest(t, server.themes, serverThemeArchive(t, "midnight", "1.0.0")); err != nil {
		t.Fatal(err)
	}
	if err := server.themes.Activate("midnight"); err != nil {
		t.Fatal(err)
	}
	archive := themeArchiveFiles(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 2.0.0\nrequires: '>=9.0.0'\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	})
	request := themeUploadRequest(t, archive)
	recorder := httptest.NewRecorder()
	server.handleInstallTheme(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "theme_incompatible") {
		t.Fatalf("upgrade response = %d %s", recorder.Code, recorder.Body.String())
	}
	items, err := server.themes.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "midnight" && item.Version != "1.0.0" {
			t.Fatalf("incompatible upgrade kept version %q", item.Version)
		}
	}
}

func TestThemePreviewOriginUsesSiblingHost(t *testing.T) {
	server, _ := themeServerFixture(t, failingThemePublisher{})
	request := httptest.NewRequest(http.MethodPost, "https://mutiblog.nl.chrono-well.top/api/v1/admin/themes/earth/preview", nil)
	origin, err := server.previewOrigin(request)
	if err != nil || origin != "https://preview.nl.chrono-well.top" {
		t.Fatalf("preview origin = %q, %v", origin, err)
	}
}

func TestThemePreviewHostCannotReachAdministrativeSurfaces(t *testing.T) {
	server, repository := themeServerFixture(t, failingThemePublisher{})
	var site domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	site.BaseURL = "https://mutiblog.nl.chrono-well.top"
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	called := false
	handler := server.previewIsolation(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/console/", "/api/v1/auth/login", "/api/v1/setup/status", "/api/v1/admin/themes", "/health/ready"} {
		request := httptest.NewRequest(http.MethodGet, "https://preview.nl.chrono-well.top"+path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound || called {
			t.Fatalf("preview request %q = %d, called = %t", path, recorder.Code, called)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "https://preview.nl.chrono-well.top/zh-CN/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("public preview request = %d, called = %t", recorder.Code, called)
	}
}

func themeServerFixture(t *testing.T, sitePublisher SitePublisher) (*Server, *fsrepo.Repository) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", AdminLocale: "zh-CN", Timezone: "Asia/Shanghai", ActiveTheme: "earth", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "Test"}}, CreatedAt: now, UpdatedAt: now}, false); err != nil {
		t.Fatal(err)
	}
	return &Server{repository: repository, themes: themes.NewService(repository), publisher: sitePublisher, audit: audit.NewService(repository), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, repository
}

func serverThemeArchive(t *testing.T, id, version string) *bytes.Reader {
	t.Helper()
	return themeArchiveFiles(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: " + id + "\nname: Midnight\nversion: " + version + "\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	})
}

func themeArchiveFiles(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, contents := range files {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buffer.Bytes())
}

func themeUploadRequest(t *testing.T, archive io.Reader) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "theme.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, archive); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/install", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return request
}
