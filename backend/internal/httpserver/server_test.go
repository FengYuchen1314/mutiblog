package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/auth"
	"github.com/fengyuchen/mutiblog/internal/backup"
	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/events"
	"github.com/fengyuchen/mutiblog/internal/index"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/state"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
	"github.com/fengyuchen/mutiblog/internal/theme"
	"github.com/go-chi/chi/v5"
)

func TestSetupLoginAndProtectedRoute(t *testing.T) {
	root := t.TempDir()
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(root, "data", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{}
	cfg.Security.SessionSecret = "01234567890123456789012345678901"
	cfg.Security.SessionMaxAge = 3600
	cfg.Security.CookieSecure = "false"
	cfg.I18n.DefaultLocale = "en"
	h := New(
		&Server{
			Config:  cfg,
			State:   db,
			Users:   users,
			Index:   index.New(index.Options{ContentRoot: filepath.Join(root, "content")}),
			Content: content.NewStore(filepath.Join(root, "content")),
			Events:  events.New(nil),
		},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	body := []byte(
		`{"username":"admin","email":"admin@example.test","password":"correct horse battery staple","locale":"en"}`,
	)
	request = httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("me: %d %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/auth/csrf", nil)
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("csrf: %d", response.Code)
	}
	csrfCookie := response.Result().Cookies()[0]
	var csrfPayload struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &csrfPayload); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(
		http.MethodPost,
		"/api/admin/posts/",
		bytes.NewReader([]byte(`{"title":"First post","body":"Hello"}`)),
	)
	request.AddCookie(cookies[0])
	request.AddCookie(csrfCookie)
	request.Header.Set("X-CSRF-Token", csrfPayload.Data.Token)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create post: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(
		http.MethodPost,
		"/api/admin/posts/"+created.Data.ID+"/publish",
		bytes.NewBufferString(`{"publishAt":"2030-01-02T03:04:05Z"}`),
	)
	request.AddCookie(cookies[0])
	request.AddCookie(csrfCookie)
	request.Header.Set("X-CSRF-Token", csrfPayload.Data.Token)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"scheduled":true`) {
		t.Fatalf("schedule publish: %d %s", response.Code, response.Body.String())
	}
	var status string
	row := db.Read().QueryRow("SELECT status FROM scheduled_publish WHERE article_id=?", created.Data.ID)
	if err := row.Scan(&status); err != nil || status != "scheduled" {
		t.Fatalf("scheduled row = %q, %v", status, err)
	}
	request = httptest.NewRequest(
		http.MethodPut,
		"/api/admin/posts/"+created.Data.ID,
		bytes.NewBufferString(`{"title":"First post","body":"new body","baseHash":"stale"}`),
	)
	request.AddCookie(cookies[0])
	request.AddCookie(csrfCookie)
	request.Header.Set("X-CSRF-Token", csrfPayload.Data.Token)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected edit conflict: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPut, "/api/admin/users/admin", bytes.NewBufferString(`{"disabled":true}`))
	request.AddCookie(cookies[0])
	request.AddCookie(csrfCookie)
	request.Header.Set("X-CSRF-Token", csrfPayload.Data.Token)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("last admin protection: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login",
		bytes.NewReader([]byte(`{"username":"admin","password":"correct horse battery staple"}`)),
	)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("login: %d %s", response.Code, response.Body.String())
	}
	var loginAttempts, auditEvents int
	attemptRow := db.Read().QueryRow(
		"SELECT count(*) FROM login_attempts WHERE identifier='admin' AND success=1",
	)
	if err := attemptRow.Scan(&loginAttempts); err != nil || loginAttempts != 1 {
		t.Fatalf("login attempts=%d err=%v", loginAttempts, err)
	}
	auditRow := db.Read().QueryRow("SELECT count(*) FROM audit_log WHERE action='auth.login'")
	if err := auditRow.Scan(&auditEvents); err != nil || auditEvents != 1 {
		t.Fatalf("login audit=%d err=%v", auditEvents, err)
	}
	logSQL := "INSERT INTO system_log(level,component,message,created_at) " +
		"VALUES('warning','render','renderer delayed','2026-08-10T00:00:00Z')"
	if _, err := db.Write().Exec(logSQL); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/system/logs?limit=1", nil)
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "renderer delayed") {
		t.Fatalf("system logs: %d %s", response.Code, response.Body.String())
	}
}

func TestSetArticleStatusPersistsEveryLocale(t *testing.T) {
	root := t.TempDir()
	store := content.NewStore(filepath.Join(root, "content"))
	front := model.FrontMatter{
		Title:        "Source",
		Slug:         "source",
		Status:       model.StatusPublished,
		Author:       "admin",
		SourceLocale: "en",
	}
	article, err := store.CreateBundle(model.ContentPost, "en", front, "source body")
	if err != nil {
		t.Fatal(err)
	}
	translated := front
	translated.Title, translated.Slug = "Traduction", "traduction"
	if err := store.SaveVersion(
		article, "fr", translated, "translated body",
		content.SaveOpts{MirrorAuthoritative: true},
	); err != nil {
		t.Fatal(err)
	}
	ix := index.New(index.Options{ContentRoot: filepath.Join(root, "content")})
	ix.UpsertArticle(article)
	server := &Server{Content: store, Index: ix}
	if err := server.setArticleStatus(article, model.StatusTrashed); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadBundle(article.BundleDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SourceRev != 1 {
		t.Fatalf("source revision=%d", loaded.SourceRev)
	}
	for locale, version := range loaded.Versions {
		if version.Front.Status != model.StatusTrashed {
			t.Fatalf("%s status=%s", locale, version.Front.Status)
		}
	}
	if got := server.nextCopySlug(model.ContentPost, "en", "source"); got != "source-copy" {
		t.Fatalf("copy slug=%q", got)
	}
}

func TestSettingsAPIUsesMaskedReadAndAtomicSectionUpdate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	configBody := `server: { baseURL: https://example.test, port: 8080 }
site: { title: Before }
i18n: { defaultLocale: en, sourceLocale: en, locales: [{code: en, name: English, urlPrefix: en, enabled: true}] }
security: { sessionSecret: "01234567890123456789012345678901" }
ai: { apiKey: "test-api-key" }
`
	if err := os.WriteFile(filepath.Join(root, "config", "config.yaml"), []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := users.CreateAdmin("admin", "admin@example.test", "correct horse battery staple", "en")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Sign(cfg.Security.SessionSecret, auth.NewClaims(admin, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	csrf := "csrf-value"
	h := New(
		&Server{
			Root:    root,
			Config:  cfg,
			Users:   users,
			Index:   index.New(index.Options{ContentRoot: filepath.Join(root, "content")}),
			Content: content.NewStore(filepath.Join(root, "content")),
			Events:  events.New(nil),
		},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/settings/", nil)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "test-api-key") ||
		!strings.Contains(response.Body.String(), "********") {
		t.Fatalf("masked settings = %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(
		http.MethodPut,
		"/api/admin/settings/site",
		bytes.NewBufferString(`{"title":"After"}`),
	)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	request.Header.Set("X-CSRF-Token", csrf)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("settings update = %d %s", response.Code, response.Body.String())
	}
	if cfg.Site.Title != "After" {
		t.Fatalf("live config title=%q", cfg.Site.Title)
	}
	request = httptest.NewRequest(
		http.MethodPut,
		"/api/admin/settings/security",
		bytes.NewBufferString(`{"sessionSecret":"********"}`),
	)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	request.Header.Set("X-CSRF-Token", csrf)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("masked secret update = %d %s", response.Code, response.Body.String())
	}
	reloaded, _, err := config.Load(root, "")
	if err != nil || reloaded.Security.SessionSecret != cfg.Security.SessionSecret {
		t.Fatalf("secret was overwritten: %q, %v", reloaded.Security.SessionSecret, err)
	}
}

func TestSystemAuditFiltersEntries(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertAudit := "INSERT INTO audit_log(actor,action,target_type,target_id,detail,ip,created_at) " +
		"VALUES(?,?,?,?,?,?,?)"
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Write().Exec(
		insertAudit, "admin", "theme.activate", "theme", "default", "{}", "127.0.0.1", now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().Exec(
		insertAudit, "editor", "post.update", "post", "x", "{}", "127.0.0.1", now,
	); err != nil {
		t.Fatal(err)
	}
	server := &Server{State: db}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/system/audit?actor=admin&action=theme.activate", nil)
	response := httptest.NewRecorder()
	server.systemAudit(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"theme.activate"`) ||
		strings.Contains(response.Body.String(), `"post.update"`) {
		t.Fatalf("audit = %d %s", response.Code, response.Body.String())
	}
}

func TestDownloadBackupLimitsNameToBackupDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "safe.zip"), []byte("zip-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{Backup: backup.Options{OutputDir: root}}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/backups/safe.zip/download", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("name", "safe.zip")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	server.downloadBackup(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "zip-data" ||
		response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("download = %d %q %q", response.Code, response.Body.String(), response.Header().Get("Content-Type"))
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/backups/nope/download", nil)
	route = chi.NewRouteContext()
	route.URLParams.Add("name", "../safe.zip")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	response = httptest.NewRecorder()
	server.downloadBackup(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unsafe download = %d", response.Code)
	}
}

func TestRestoreBackupRequiresExplicitConfirmation(t *testing.T) {
	server := &Server{Root: t.TempDir(), Backup: backup.Options{OutputDir: t.TempDir()}}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/backups/restore",
		strings.NewReader(`{"name":"missing.zip","confirm":false}`),
	)
	response := httptest.NewRecorder()
	server.restoreBackup(response, request)
	if response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), "RESTORE_CONFIRMATION_REQUIRED") {
		t.Fatalf("restore confirmation = %d %s", response.Code, response.Body.String())
	}
}

func TestRestartRendererReportsUnavailableWithoutService(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/admin/system/restart-renderer", nil)
	response := httptest.NewRecorder()
	(&Server{}).restartRenderer(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), "RENDERER_UNAVAILABLE") {
		t.Fatalf("restart renderer = %d %s", response.Code, response.Body.String())
	}
}

func TestSystemStatsWorksWithoutOptionalWorkers(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/admin/system/stats", nil)
	response := httptest.NewRecorder()
	(&Server{}).systemStats(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"rendererRunning":false`) {
		t.Fatalf("stats = %d %s", response.Code, response.Body.String())
	}
}

func TestThemeActivationAndSettingsResetAPI(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	configBody := `server: { baseURL: https://example.test, port: 8080 }
site: { title: Before }
i18n: { defaultLocale: en, sourceLocale: en, locales: [{code: en, name: English, urlPrefix: en, enabled: true}] }
security: { sessionSecret: "01234567890123456789012345678901" }
theme: { active: default }
`
	if err := os.WriteFile(filepath.Join(root, "config", "config.yaml"), []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default", "demo"} {
		dir := filepath.Join(root, "themes", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "name: " + name +
			"\ntemplates: [home, post, page, category, category_list, tag, tag_list, archive, links, search, not_found]\n"
		if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		schema := `{"fields":[{"key":"color","type":"color","default":"#112233"}]}`
		if err := os.WriteFile(filepath.Join(dir, "settings.schema.json"), []byte(schema), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := users.CreateAdmin("admin", "admin@example.test", "correct horse battery staple", "en")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Sign(cfg.Security.SessionSecret, auth.NewClaims(admin, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	store := theme.NewStore(filepath.Join(root, "themes"), filepath.Join(root, "data"))
	h := New(
		&Server{
			Root:    root,
			Config:  cfg,
			Users:   users,
			Index:   index.New(index.Options{ContentRoot: filepath.Join(root, "content")}),
			Content: content.NewStore(filepath.Join(root, "content")),
			Events:  events.New(nil),
			Theme:   store,
		},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/themes/demo", nil)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"active":false`) {
		t.Fatalf("theme detail = %d %s", response.Code, response.Body.String())
	}
	csrf := "csrf-value"
	request = httptest.NewRequest(http.MethodPost, "/api/admin/themes/demo/activate", nil)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	request.Header.Set("X-CSRF-Token", csrf)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || cfg.Theme.Active != "demo" {
		t.Fatalf("activate = %d %s active=%q", response.Code, response.Body.String(), cfg.Theme.Active)
	}
	request = httptest.NewRequest(
		http.MethodPut,
		"/api/admin/themes/demo/settings",
		bytes.NewBufferString(`{"color":"#abcdef"}`),
	)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	request.Header.Set("X-CSRF-Token", csrf)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save theme settings = %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/admin/themes/demo/settings/reset", nil)
	request.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	request.Header.Set("X-CSRF-Token", csrf)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "#112233") {
		t.Fatalf("reset theme settings = %d %s", response.Code, response.Body.String())
	}
	reloaded, _, err := config.Load(root, "")
	if err != nil || reloaded.Theme.Active != "demo" {
		t.Fatalf("persisted theme = %#v, %v", reloaded.Theme, err)
	}
}

func TestLinksAndMenusAPI(t *testing.T) {
	root := t.TempDir()
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := users.CreateAdmin("admin", "admin@example.test", "correct horse battery staple", "en")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Security.SessionSecret = "01234567890123456789012345678901"
	cfg.Security.SessionMaxAge = 3600
	cfg.Security.CookieSecure = "false"
	cfg.I18n.DefaultLocale, cfg.I18n.SourceLocale = "en", "en"
	tax := taxonomy.NewStore(filepath.Join(root, "data"))
	ix := index.New(index.Options{ContentRoot: filepath.Join(root, "content")})
	if err = ix.RebuildAll(context.Background(), content.NewStore(filepath.Join(root, "content")), tax); err != nil {
		t.Fatal(err)
	}
	h := New(
		&Server{
			Config:   cfg,
			Users:    users,
			Index:    ix,
			Content:  content.NewStore(filepath.Join(root, "content")),
			Taxonomy: tax,
			Events:   events.New(nil),
		},
	)
	session, err := auth.Sign(cfg.Security.SessionSecret, auth.NewClaims(admin, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	csrf := "test-csrf-token"
	write := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
		req.Header.Set("X-CSRF-Token", csrf)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	linkJSON := `{"id":"example","name":"Example","url":"https://example.com"}`
	if res := write("/api/admin/links/", linkJSON); res.Code != http.StatusOK {
		t.Fatalf("create link: %d %s", res.Code, res.Body.String())
	}
	if res := write("/api/admin/menus/", `{"id":"main","name":{"en":"Main"},"items":[]}`); res.Code != http.StatusOK {
		t.Fatalf("create menu: %d %s", res.Code, res.Body.String())
	}
	for _, path := range []string{"/api/admin/links/", "/api/admin/menus/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "blog_session", Value: session})
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusOK ||
			!strings.Contains(res.Body.String(), "example") && strings.Contains(path, "links") {
			t.Fatalf("list %s: %d %s", path, res.Code, res.Body.String())
		}
	}
}

func TestRootLocaleRedirectAndStaticFiles(t *testing.T) {
	root := t.TempDir()
	generated := filepath.Join(root, "generated")
	if err := os.MkdirAll(filepath.Join(generated, "public", "zh-tw"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := []byte("traditional")
	if err := os.WriteFile(filepath.Join(generated, "public", "zh-tw", "index.html"), home, 0o644); err != nil {
		t.Fatal(err)
	}
	notFound := []byte("localized not found")
	if err := os.WriteFile(filepath.Join(generated, "public", "zh-tw", "404.html"), notFound, 0o644); err != nil {
		t.Fatal(err)
	}
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Paths.Generated = generated
	cfg.Server.ServeStatic = true
	cfg.Server.TrustedProxies = []string{"127.0.0.1"}
	cfg.I18n.DefaultLocale = "en"
	cfg.I18n.CookieName = "locale"
	cfg.I18n.Locales = []config.LocaleConfig{
		{Code: "en", URLPrefix: "en", Enabled: true},
		{Code: "zh-TW", URLPrefix: "zh-tw", Enabled: true},
	}
	cfg.I18n.LocaleAliases = map[string]string{"zh-HK": "zh-TW"}
	cfg.I18n.CountryLocaleMap = map[string]string{"TW": "zh-TW"}
	h := New(
		&Server{
			Config:  cfg,
			Users:   users,
			Index:   index.New(index.Options{ContentRoot: filepath.Join(root, "content")}),
			Content: content.NewStore(filepath.Join(root, "content")),
			Events:  events.New(nil),
		},
	)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Language", "zh-HK, en;q=0.8")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/zh-tw/" {
		t.Fatalf("redirect = %d %q", response.Code, response.Header().Get("Location"))
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("root redirect cache policy = %q", response.Header().Get("Cache-Control"))
	}
	if cookies := response.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != "locale" ||
		cookies[0].Value != "zh-TW" {
		t.Fatalf("locale cookie = %#v", cookies)
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:443"
	request.Header.Set("CF-IPCountry", "TW")
	request.AddCookie(&http.Cookie{Name: "locale", Value: "en"})
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Header().Get("Location") != "/en/" {
		t.Fatalf("cookie precedence redirect = %q", response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "/zh-tw/", nil)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "traditional" {
		t.Fatalf("static = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/zh-tw/missing/", nil)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Body.String() != "localized not found" {
		t.Fatalf("localized 404 = %d %q", response.Code, response.Body.String())
	}
}

func TestGoDirectModeServesMedia(t *testing.T) {
	root := t.TempDir()
	mediaDir := filepath.Join(root, "media")
	if err := os.MkdirAll(filepath.Join(mediaDir, "2026", "08"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("media-bytes")
	if err := os.WriteFile(filepath.Join(mediaDir, "2026", "08", "pixel.png"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Paths.Media = mediaDir
	cfg.Paths.Generated = filepath.Join(root, "generated")
	cfg.Storage.Local.PublicPrefix = "/media"
	cfg.Server.ServeStatic = true
	cfg.Server.TrustedProxies = []string{"127.0.0.1"}
	cfg.I18n.DefaultLocale = "en"
	cfg.I18n.CookieName = "locale"
	cfg.I18n.Locales = []config.LocaleConfig{{Code: "en", URLPrefix: "en", Enabled: true}}
	h := New(
		&Server{
			Config:  cfg,
			Users:   users,
			Index:   index.New(index.Options{ContentRoot: filepath.Join(root, "content")}),
			Content: content.NewStore(filepath.Join(root, "content")),
			Events:  events.New(nil),
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/media/2026/08/pixel.png", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "media-bytes" {
		t.Fatalf("media serve = %d %q", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/media/../../etc/passwd", nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("traversal serve = %d", res.Code)
	}
}

func TestLoginRateLimitAndOriginValidation(t *testing.T) {
	root := t.TempDir()
	users, err := auth.OpenUsers(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = users.CreateAdmin("admin", "admin@example.test", "correct horse battery staple", "en"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Security.SessionSecret = "01234567890123456789012345678901"
	cfg.Security.LoginRateLimit.Attempts = 1
	cfg.Security.LoginRateLimit.Window = config.Duration(time.Minute)
	cfg.Server.BaseURL = "https://blog.example.test"
	h := New(
		&Server{
			Config:  cfg,
			Users:   users,
			Index:   index.New(index.Options{ContentRoot: filepath.Join(root, "content")}),
			Content: content.NewStore(filepath.Join(root, "content")),
			Events:  events.New(nil),
		},
	)
	for _, want := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/auth/login",
			bytes.NewBufferString(`{"username":"admin","password":"wrong"}`),
		)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("login status=%d, want=%d", response.Code, want)
		}
	}
	server := &Server{Config: cfg}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/posts/", nil)
	request.Header.Set("Origin", "https://evil.example")
	if server.sameOrigin(request) {
		t.Fatal("accepted cross-origin request")
	}
	request.Header.Set("Origin", "https://blog.example.test")
	if !server.sameOrigin(request) {
		t.Fatal("rejected matching origin")
	}
}

func TestReplaceCategoryPreservesOrderAndRemovesDuplicates(t *testing.T) {
	got := replaceString([]string{"old", "other", "new", "old"}, "old", "new")
	if strings.Join(got, ",") != "new,other" {
		t.Fatalf("categories=%v", got)
	}
}

func TestCookieSecureAutoHonorsTrustedForwardedProto(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.CookieSecure = "auto"
	cfg.Server.BaseURL = "http://blog.example.test"
	cfg.Server.TrustedProxies = []string{"127.0.0.1"}
	server := &Server{Config: cfg}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:443"
	request.Header.Set("X-Forwarded-Proto", "https")
	if !server.cookieSecure(request) {
		t.Fatal("trusted https proxy should set Secure cookie")
	}
	request.RemoteAddr = "198.51.100.2:443"
	if server.cookieSecure(request) {
		t.Fatal("untrusted forwarded header must not control cookie security")
	}
}

func TestCookieSecureAutoUsesRequestTransport(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.CookieSecure = "auto"
	cfg.Server.BaseURL = "https://blog.example.test"
	server := &Server{Config: cfg}
	request := httptest.NewRequest(http.MethodGet, "http://blog.example.test/", nil)
	if server.cookieSecure(request) {
		t.Fatal("plain HTTP request must not receive a Secure cookie in auto mode")
	}
}

func TestSlugifyPreservesCJK(t *testing.T) {
	if got := slugify("我的 服务器搭建记录!"); got != "我的-服务器搭建记录" {
		t.Fatalf("slug=%q", got)
	}
}
