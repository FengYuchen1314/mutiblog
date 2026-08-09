// Package httpserver owns HTTP routing, API response conventions, and security boundaries.
package httpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fengyuchen/mutiblog/internal/ai"
	"github.com/fengyuchen/mutiblog/internal/auth"
	"github.com/fengyuchen/mutiblog/internal/backup"
	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/events"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	blogi18n "github.com/fengyuchen/mutiblog/internal/i18n"
	"github.com/fengyuchen/mutiblog/internal/importer"
	"github.com/fengyuchen/mutiblog/internal/index"
	"github.com/fengyuchen/mutiblog/internal/jobs"
	"github.com/fengyuchen/mutiblog/internal/media"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/render"
	"github.com/fengyuchen/mutiblog/internal/state"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
	"github.com/fengyuchen/mutiblog/internal/theme"
	"github.com/fengyuchen/mutiblog/web"
	"github.com/go-chi/chi/v5"
	"golang.org/x/text/language"
)

type Server struct {
	Root, ConfigFile string
	Config           *config.Config
	Users            *auth.Users
	Index            *index.Index
	Content          *content.Store
	Events           *events.Bus
	Media            *media.LocalStorage
	Taxonomy         *taxonomy.Store
	Render           *render.Service
	Jobs             *jobs.Queue
	AI               *ai.Service
	Theme            *theme.Store
	AppVersion       string
	Backup           backup.Options
	State            *state.DB
	limiter          *rateLimiter
	dummyHash        string
}
type ctxKey int

const userKey ctxKey = iota

func New(s *Server) http.Handler {
	if s.limiter == nil {
		s.limiter = newRateLimiter()
	}
	if s.dummyHash == "" {
		s.dummyHash, _ = auth.HashPassword("dummy credential for timing equalization")
	}
	r := chi.NewRouter()
	r.Use(requestID, securityHeaders)
	registry, err := blogi18n.New(s.Config.I18n)
	if err != nil {
		registry = nil
	}
	r.Get("/", s.rootRedirect(registry))
	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusPermanentRedirect)
	})
	r.Handle("/admin/*", http.FileServer(http.FS(web.Admin)))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { ok(w, http.StatusOK, map[string]bool{"ok": true}) })
	r.Route("/api/auth", func(r chi.Router) {
		r.Get("/status", s.status)
		r.Post("/setup", s.setup)
		r.Post("/login", s.login)
		r.Post("/logout", s.logout)
		r.Get("/csrf", s.requireAuth(s.csrf))
	})
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(s.apiLimit)
		r.Use(s.authMiddleware)
		r.Get("/me", s.me)
		r.Get("/dashboard", s.dashboard)
		r.With(s.perm("log.read")).Get("/system/health", s.systemHealth)
		r.With(s.perm("log.read")).Get("/system/logs", s.systemLogs)
		r.With(s.perm("log.read")).Get("/system/audit", s.systemAudit)
		r.With(s.perm("log.read")).Get("/system/index-errors", s.indexErrors)
		r.With(s.perm("log.read")).Get("/system/stats", s.systemStats)
		r.With(s.perm("render.rebuild")).Post("/system/reindex", s.reindex)
		r.With(s.perm("render.rebuild")).Post("/system/restart-renderer", s.restartRenderer)
		r.With(s.perm("post.read")).Post("/preview/markdown", s.previewMarkdown)
		r.Route("/posts", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/", s.posts)
			r.With(s.perm("post.write")).Post("/", s.createPost)
			r.With(s.perm("post.read")).Get("/{id}", s.post)
			r.With(s.perm("post.write")).Put("/{id}", s.updatePost)
			r.With(s.perm("post.publish")).Post("/{id}/publish", s.publishPost)
			r.With(s.perm("post.publish")).Post("/{id}/unpublish", s.unpublishPost)
			r.With(s.perm("post.write")).Post("/{id}/restore", s.restorePost)
			r.With(s.perm("post.write")).Post("/{id}/duplicate", s.duplicatePost)
			r.With(s.perm("post.delete")).Delete("/{id}", s.deletePost)
			r.With(s.perm("post.delete")).Delete("/{id}/purge", s.purgePost)
			r.With(s.perm("post.read")).Get("/{id}/revisions", s.revisions)
			r.With(s.perm("post.read")).Get("/{id}/revisions/{rev}", s.revision)
			r.With(s.perm("post.write")).Post("/{id}/revisions/{rev}/restore", s.restoreRevision)
			r.With(s.perm("post.write")).Post("/{id}/draft", s.saveDraft)
		})
		r.Route("/pages", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/", s.pages)
			r.With(s.perm("post.write")).Post("/", s.createPage)
			r.With(s.perm("post.read")).Get("/{id}", s.post)
			r.With(s.perm("post.write")).Put("/{id}", s.updatePost)
			r.With(s.perm("post.publish")).Post("/{id}/publish", s.publishPost)
			r.With(s.perm("post.publish")).Post("/{id}/unpublish", s.unpublishPost)
			r.With(s.perm("post.write")).Post("/{id}/restore", s.restorePost)
			r.With(s.perm("post.write")).Post("/{id}/duplicate", s.duplicatePost)
			r.With(s.perm("post.delete")).Delete("/{id}", s.deletePost)
			r.With(s.perm("post.delete")).Delete("/{id}/purge", s.purgePost)
			r.With(s.perm("post.write")).Post("/{id}/draft", s.saveDraft)
		})
		r.Route("/media", func(r chi.Router) {
			r.With(s.perm("media.write")).Get("/", s.listMedia)
			r.With(s.perm("media.write")).Post("/upload", s.uploadMedia)
			r.With(s.perm("media.write")).Post("/mkdir", s.mkdirMedia)
			r.With(s.perm("media.write")).Delete("/", s.deleteMedia)
		})
		r.Route("/categories", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/", s.categories)
			r.With(s.perm("taxonomy.write")).Post("/", s.saveCategory)
			r.With(s.perm("taxonomy.write")).Put("/{id}", s.saveCategory)
			r.With(s.perm("taxonomy.write")).Delete("/{id}", s.deleteCategory)
		})
		r.Route("/tags", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/", s.tags)
			r.With(s.perm("taxonomy.write")).Post("/", s.saveTag)
			r.With(s.perm("taxonomy.write")).Put("/{id}", s.saveTag)
			r.With(s.perm("taxonomy.write")).Delete("/{id}", s.deleteTag)
		})
		r.Route("/links", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/", s.links)
			r.With(s.perm("taxonomy.write")).Post("/", s.saveLink)
			r.With(s.perm("post.read")).Get("/groups", s.linkGroups)
			r.With(s.perm("taxonomy.write")).Put("/groups", s.saveLinkGroups)
			r.With(s.perm("taxonomy.write")).Put("/reorder", s.reorderLinks)
			r.With(s.perm("post.read")).Get("/{id}", s.link)
			r.With(s.perm("taxonomy.write")).Put("/{id}", s.saveLink)
			r.With(s.perm("taxonomy.write")).Delete("/{id}", s.deleteLink)
		})
		r.Route("/menus", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/", s.menus)
			r.With(s.perm("taxonomy.write")).Post("/", s.saveMenu)
			r.With(s.perm("post.read")).Get("/targets", s.menuTargets)
			r.With(s.perm("post.read")).Get("/{id}", s.menu)
			r.With(s.perm("taxonomy.write")).Put("/{id}", s.saveMenu)
			r.With(s.perm("taxonomy.write")).Delete("/{id}", s.deleteMenu)
		})
		r.Route("/translations", func(r chi.Router) {
			r.With(s.perm("post.read")).Get("/tasks", s.translationTasks)
			r.With(s.perm("post.translate")).Post("/tasks", s.createTranslationTask)
			r.With(s.perm("post.translate")).Post("/test", s.testTranslationProvider)
		})
		r.Route("/themes", func(r chi.Router) {
			r.With(s.perm("theme.settings")).Get("/", s.themes)
			r.With(s.perm("theme.settings")).Post("/reload", s.reloadThemes)
			r.With(s.perm("theme.settings")).Get("/{name}", s.themeDetail)
			r.With(s.perm("theme.settings")).Get("/{name}/settings", s.themeSettings)
			r.With(s.perm("theme.settings")).Put("/{name}/settings", s.saveThemeSettings)
			r.With(s.perm("theme.settings")).Post("/{name}/settings/reset", s.resetThemeSettings)
			r.With(s.perm("theme.activate")).Post("/{name}/activate", s.activateTheme)
		})
		r.Route("/backups", func(r chi.Router) {
			r.With(s.perm("backup.manage")).Get("/", s.backups)
			r.With(s.perm("backup.manage")).Post("/", s.createBackup)
			r.With(s.perm("backup.manage")).Post("/restore", s.restoreBackup)
			r.With(s.perm("backup.manage")).Get("/{name}/download", s.downloadBackup)
			r.With(s.perm("backup.manage")).Delete("/{name}", s.deleteBackup)
		})
		r.With(s.perm("post.write")).Post("/import", s.createImport)
		r.With(s.perm("post.read")).Get("/import/{jobID}", s.importStatus)
		r.Route("/settings", func(r chi.Router) {
			r.With(s.perm("settings.write")).Get("/", s.settings)
			r.With(s.perm("settings.write")).Put("/{section}", s.saveSettings)
		})
		r.Route("/users", func(r chi.Router) {
			r.With(s.perm("user.manage")).Get("/", s.listUsers)
			r.With(s.perm("user.manage")).Post("/", s.createUser)
			r.With(s.perm("user.manage")).Put("/{id}", s.updateUser)
			r.With(s.perm("user.manage")).Delete("/{id}", s.deleteUser)
			r.With(s.perm("user.manage")).Post("/{id}/password", s.resetUserPassword)
		})
	})
	if s.Config.Server.ServeStatic {
		r.Handle("/*", s.staticHandler())
	}
	return r
}

func (s *Server) staticHandler() http.Handler {
	root := filepath.Join(s.Config.Paths.Generated, "public")
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if rel != "" {
			candidate, err := fsutil.SafeJoin(root, rel)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			info, statErr := os.Stat(candidate)
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					s.serveLocalizedNotFound(w, r, root)
				} else {
					http.NotFound(w, r)
				}
				return
			}
			if info.IsDir() {
				if _, err := os.Stat(filepath.Join(candidate, "index.html")); errors.Is(err, os.ErrNotExist) {
					s.serveLocalizedNotFound(w, r, root)
					return
				}
			}
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) serveLocalizedNotFound(w http.ResponseWriter, r *http.Request, root string) {
	prefix := ""
	first := strings.Split(strings.TrimPrefix(filepath.Clean(r.URL.Path), "/"), "/")[0]
	for _, locale := range s.Config.I18n.Locales {
		if locale.URLPrefix == first {
			prefix = locale.URLPrefix
			break
		}
	}
	if prefix == "" {
		for _, locale := range s.Config.I18n.Locales {
			if locale.Code == s.Config.I18n.DefaultLocale {
				prefix = locale.URLPrefix
				break
			}
		}
	}
	if prefix == "" {
		http.NotFound(w, r)
		return
	}
	path, err := fsutil.SafeJoin(root, filepath.ToSlash(filepath.Join(prefix, "404.html")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

type rateBucket struct {
	hits       []time.Time
	lockedTill time.Time
}
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: map[string]*rateBucket{}} }
func (l *rateLimiter) allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	now := time.Now()
	cutoff := now.Add(-window)
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[key]
	if bucket == nil {
		bucket = &rateBucket{}
		l.buckets[key] = bucket
	}
	kept := bucket.hits[:0]
	for _, hit := range bucket.hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	bucket.hits = kept
	if len(bucket.hits) >= limit {
		return false, time.Until(bucket.hits[0].Add(window))
	}
	bucket.hits = append(bucket.hits, now)
	return true, 0
}

// allowLogin enforces an explicit lockout after the configured number of
// attempts. The key contains both the canonical username and the client IP so
// one noisy account does not lock every administrator sharing an address.
func (l *rateLimiter) allowLogin(key string, limit int, window, lockout time.Duration) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	if lockout <= 0 {
		// Hand-constructed test and extension configs may omit lockout; retaining
		// the rate window is the safe fallback rather than silently disabling it.
		lockout = window
	}
	now := time.Now()
	cutoff := now.Add(-window)
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[key]
	if bucket == nil {
		bucket = &rateBucket{}
		l.buckets[key] = bucket
	}
	if bucket.lockedTill.After(now) {
		return false, time.Until(bucket.lockedTill)
	}
	kept := bucket.hits[:0]
	for _, hit := range bucket.hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	bucket.hits = kept
	if len(bucket.hits) >= limit {
		bucket.hits = nil
		bucket.lockedTill = now.Add(lockout)
		return false, lockout
	}
	bucket.hits = append(bucket.hits, now)
	return true, 0
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func (s *Server) apiLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := s.Config.Security.APIRateLimit.Burst
		if limit == 0 {
			limit = 50
		}
		if ok, retry := s.limiter.allow("api:"+clientIP(r), limit, time.Second); !ok {
			w.Header().Set("Retry-After", fmt.Sprint(maxInt(1, int(retry.Seconds()))))
			fail(w, http.StatusTooManyRequests, "RATE_LIMITED", "API request limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Server) rootRedirect(registry *blogi18n.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		locale := s.negotiateLocale(r, registry)
		prefix := strings.ToLower(string(locale))
		if registry != nil {
			prefix = registry.URLPrefix(locale)
		}
		cookieName := s.localeCookieName()
		maxAge := s.Config.I18n.CookieMaxAge
		if maxAge == 0 {
			maxAge = 365 * 24 * 60 * 60
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: string(locale), Path: "/", MaxAge: maxAge, SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure(r)})
		http.Redirect(w, r, "/"+prefix+"/", http.StatusFound)
	}
}

// negotiateLocale follows the public-site precedence: explicit cookie, browser
// language preferences, trusted edge country hint, and finally the default.
func (s *Server) negotiateLocale(r *http.Request, registry *blogi18n.Registry) model.Locale {
	defaultLocale := model.Locale(s.Config.I18n.DefaultLocale)
	if defaultLocale == "" {
		defaultLocale = model.Locale(s.Config.I18n.SourceLocale)
	}
	if registry == nil {
		return defaultLocale
	}
	resolve := func(value string) (model.Locale, bool) {
		if locale, ok := registry.Canonical(value); ok && registry.Enabled(locale) {
			return locale, true
		}
		if locale, ok := registry.Alias(value); ok && registry.Enabled(locale) {
			return locale, true
		}
		return "", false
	}
	if cookie, err := r.Cookie(s.localeCookieName()); err == nil {
		if locale, ok := resolve(cookie.Value); ok {
			return locale
		}
	}
	if tags, _, err := language.ParseAcceptLanguage(r.Header.Get("Accept-Language")); err == nil {
		for _, tag := range tags {
			if locale, ok := resolve(tag.String()); ok {
				return locale
			}
			base, _ := tag.Base()
			for _, enabled := range registry.Locales() {
				enabledTag, err := language.Parse(string(enabled))
				if err != nil {
					continue
				}
				enabledBase, _ := enabledTag.Base()
				if enabledBase == base {
					return enabled
				}
			}
		}
	}
	if s.requestFromTrustedProxy(r) {
		if locale, ok := registry.CountryLocale(r.Header.Get("CF-IPCountry")); ok && registry.Enabled(locale) {
			return locale
		}
	}
	if locale, ok := resolve(string(defaultLocale)); ok {
		return locale
	}
	return defaultLocale
}

func (s *Server) localeCookieName() string {
	if s.Config.I18n.CookieName != "" {
		return s.Config.I18n.CookieName
	}
	return "blog_locale"
}

func (s *Server) requestFromTrustedProxy(r *http.Request) bool {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	remote := net.ParseIP(remoteHost)
	if remote == nil {
		return false
	}
	for _, entry := range s.Config.Server.TrustedProxies {
		if ip := net.ParseIP(entry); ip != nil && ip.Equal(remote) {
			return true
		}
		if _, cidr, err := net.ParseCIDR(entry); err == nil && cidr.Contains(remote) {
			return true
		}
	}
	return false
}
func ok(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func failWithData(w http.ResponseWriter, status int, code, message string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}, "data": data})
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ok(w, http.StatusOK, map[string]bool{"setupRequired": s.Users.Empty()})
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.Users.Empty() {
		fail(w, http.StatusConflict, "ALREADY_SETUP", "installation is already complete")
		return
	}
	var req struct{ Username, Email, Password, Locale string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if req.Locale == "" {
		req.Locale = s.Config.I18n.DefaultLocale
	}
	user, err := s.Users.CreateAdmin(req.Username, req.Email, req.Password, req.Locale)
	if err != nil {
		fail(w, 422, "INVALID_INPUT", err.Error())
		return
	}
	s.setSession(w, r, user)
	s.audit(r, user.Username, "auth.setup", "user", user.ID, map[string]any{"username": user.Username})
	ok(w, http.StatusCreated, publicUser(user))
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.Users.Empty() {
		fail(w, http.StatusLocked, "SETUP_REQUIRED", "complete setup first")
		return
	}
	var req struct{ Username, Password string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	loginKey := "login:" + strings.ToLower(strings.TrimSpace(req.Username)) + ":" + clientIP(r)
	limit := s.Config.Security.LoginRateLimit
	if ok, retry := s.limiter.allowLogin(loginKey, limit.Attempts, limit.Window.Duration(), limit.Lockout.Duration()); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(maxInt(1, int(retry.Seconds()))))
		fail(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many login attempts; try again later")
		return
	}
	user, exists := s.Users.ByUsername(req.Username)
	if !exists {
		_ = auth.VerifyPassword(s.dummyHash, req.Password)
		s.recordLogin(r, req.Username, false)
		s.audit(r, req.Username, "auth.login_failed", "user", req.Username, nil)
		fail(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	if user.Disabled || !auth.VerifyPassword(user.PasswordHash, req.Password) {
		s.recordLogin(r, req.Username, false)
		s.audit(r, req.Username, "auth.login_failed", "user", user.ID, nil)
		fail(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	s.setSession(w, r, user)
	s.recordLogin(r, user.Username, true)
	s.audit(r, user.Username, "auth.login", "user", user.ID, nil)
	ok(w, http.StatusOK, publicUser(user))
}

func (s *Server) recordLogin(r *http.Request, identifier string, success bool) {
	if s.State == nil {
		return
	}
	_, _ = s.State.Write().ExecContext(r.Context(), "INSERT INTO login_attempts(identifier,ip,success,user_agent,created_at) VALUES(?,?,?,?,?)", identifier, clientIP(r), success, r.UserAgent(), time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *Server) audit(r *http.Request, actor, action, targetType, targetID string, detail any) {
	if s.State == nil {
		return
	}
	encoded, _ := json.Marshal(detail)
	_, _ = s.State.Write().ExecContext(r.Context(), "INSERT INTO audit_log(actor,action,target_type,target_id,detail,ip,created_at) VALUES(?,?,?,?,?,?,?)", actor, action, targetType, targetID, string(encoded), clientIP(r), time.Now().UTC().Format(time.RFC3339Nano))
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "blog_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure(r)})
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) csrf(w http.ResponseWriter, r *http.Request) {
	token := make([]byte, 32)
	_, _ = rand.Read(token)
	value := base64.RawURLEncoding.EncodeToString(token)
	http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: value, Path: "/", MaxAge: s.Config.Security.SessionMaxAge, SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure(r)})
	ok(w, http.StatusOK, map[string]string{"token": value})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	ok(w, http.StatusOK, publicUser(r.Context().Value(userKey).(*model.User)))
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	stats := s.Index.Stats()
	ok(w, http.StatusOK, map[string]any{"articles": stats.Articles, "posts": stats.Posts, "pages": stats.Pages, "locales": stats.Locales})
}

func (s *Server) systemHealth(w http.ResponseWriter, r *http.Request) {
	warnings := make([]map[string]string, 0)
	if s.Jobs != nil {
		stats, err := s.Jobs.Stats(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, "HEALTH_CHECK_FAILED", "system health could not be checked")
			return
		}
		if stats.Pending > 10_000 {
			warnings = append(warnings, map[string]string{"severity": "error", "code": "QUEUE_CRITICAL", "message": fmt.Sprintf("%d queued tasks need attention", stats.Pending)})
		} else if stats.Pending > 1_000 {
			warnings = append(warnings, map[string]string{"severity": "warning", "code": "QUEUE_BACKLOG", "message": fmt.Sprintf("%d tasks are waiting", stats.Pending)})
		}
		if stats.Failed > 0 {
			warnings = append(warnings, map[string]string{"severity": "warning", "code": "FAILED_JOBS", "message": fmt.Sprintf("%d tasks failed", stats.Failed)})
		}
	}
	if s.Render != nil {
		if err := s.Render.Health(r.Context()); err != nil {
			warnings = append(warnings, map[string]string{"severity": "warning", "code": "RENDERER_UNAVAILABLE", "message": "page generation service is unavailable; publishes will retry"})
		}
	}
	ok(w, http.StatusOK, map[string]any{"warnings": warnings})
}

func (s *Server) systemLogs(w http.ResponseWriter, r *http.Request) {
	if s.State == nil {
		fail(w, http.StatusServiceUnavailable, "LOGS_UNAVAILABLE", "runtime state is unavailable")
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	type entry struct {
		ID        int64  `json:"id"`
		Level     string `json:"level"`
		Component string `json:"component"`
		Message   string `json:"message"`
		Detail    string `json:"detail,omitempty"`
		CreatedAt string `json:"createdAt"`
	}
	rows, err := s.State.Read().QueryContext(r.Context(), "SELECT id,level,component,message,COALESCE(detail,''),created_at FROM system_log ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "LOGS_READ_FAILED", "system logs could not be read")
		return
	}
	defer rows.Close()
	items := make([]entry, 0)
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.ID, &item.Level, &item.Component, &item.Message, &item.Detail, &item.CreatedAt); err != nil {
			fail(w, http.StatusInternalServerError, "LOGS_READ_FAILED", "system logs could not be read")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, "LOGS_READ_FAILED", "system logs could not be read")
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemAudit(w http.ResponseWriter, r *http.Request) {
	if s.State == nil {
		fail(w, http.StatusServiceUnavailable, "LOGS_UNAVAILABLE", "runtime state is unavailable")
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	type entry struct {
		ID         int64  `json:"id"`
		Actor      string `json:"actor"`
		Action     string `json:"action"`
		TargetType string `json:"targetType,omitempty"`
		TargetID   string `json:"targetID,omitempty"`
		Detail     string `json:"detail,omitempty"`
		IP         string `json:"ip"`
		CreatedAt  string `json:"createdAt"`
	}
	actor, action := strings.TrimSpace(r.URL.Query().Get("actor")), strings.TrimSpace(r.URL.Query().Get("action"))
	rows, err := s.State.Read().QueryContext(r.Context(), `SELECT id,actor,action,COALESCE(target_type,''),COALESCE(target_id,''),COALESCE(detail,''),ip,created_at FROM audit_log WHERE (?='' OR actor=?) AND (?='' OR action=?) ORDER BY id DESC LIMIT ?`, actor, actor, action, action, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "AUDIT_READ_FAILED", "audit log could not be read")
		return
	}
	defer rows.Close()
	items := make([]entry, 0)
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.ID, &item.Actor, &item.Action, &item.TargetType, &item.TargetID, &item.Detail, &item.IP, &item.CreatedAt); err != nil {
			fail(w, http.StatusInternalServerError, "AUDIT_READ_FAILED", "audit log could not be read")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, "AUDIT_READ_FAILED", "audit log could not be read")
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) indexErrors(w http.ResponseWriter, r *http.Request) {
	if s.Index == nil {
		fail(w, http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "content index is unavailable")
		return
	}
	items := s.Index.Errors()
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) systemStats(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"rendererRunning": s.Render != nil && s.Render.Running()}
	if s.Index != nil {
		stats := s.Index.Stats()
		data["content"] = map[string]any{"articles": stats.Articles, "posts": stats.Posts, "pages": stats.Pages, "locales": stats.Locales, "parseErrors": len(s.Index.Errors())}
	}
	if s.Jobs != nil {
		stats, err := s.Jobs.Stats(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, "STATS_READ_FAILED", "job statistics could not be read")
			return
		}
		data["jobs"] = stats
	}
	ok(w, http.StatusOK, data)
}
func (s *Server) reindex(w http.ResponseWriter, r *http.Request) {
	if s.Index == nil || s.Content == nil || s.Taxonomy == nil {
		fail(w, http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "content index is unavailable")
		return
	}
	if err := s.Index.RebuildAll(r.Context(), s.Content, s.Taxonomy); err != nil {
		fail(w, http.StatusInternalServerError, "INDEX_REBUILD_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "system.reindex", "index", "content", nil)
	ok(w, http.StatusOK, map[string]any{"articles": s.Index.Stats().Articles, "errors": len(s.Index.Errors())})
}
func (s *Server) restartRenderer(w http.ResponseWriter, r *http.Request) {
	if s.Render == nil {
		fail(w, http.StatusServiceUnavailable, "RENDERER_UNAVAILABLE", "page generation service is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.Render.Restart(ctx); err != nil {
		fail(w, http.StatusBadGateway, "RENDERER_RESTART_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "system.restart_renderer", "renderer", "node", nil)
	ok(w, http.StatusOK, map[string]bool{"restarted": true})
}

func (s *Server) previewMarkdown(w http.ResponseWriter, r *http.Request) {
	if s.Render == nil {
		fail(w, http.StatusServiceUnavailable, "RENDERER_UNAVAILABLE", "page renderer is unavailable")
		return
	}
	var input struct {
		Body     string `json:"body"`
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	source := input.Body
	if source == "" {
		source = input.Markdown
	}
	html, err := s.Render.Markdown(r.Context(), source)
	if err != nil {
		fail(w, http.StatusBadGateway, "PREVIEW_FAILED", "Markdown preview could not be generated")
		return
	}
	ok(w, http.StatusOK, map[string]string{"html": html})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	if s.Root == "" {
		fail(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "configuration file location is unavailable")
		return
	}
	values, warnings, err := config.ReadSettings(s.Root, s.ConfigFile)
	if err != nil {
		fail(w, http.StatusInternalServerError, "SETTINGS_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"values": values, "warnings": warnings})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	if s.Root == "" {
		fail(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "configuration file location is unavailable")
		return
	}
	section := chi.URLParam(r, "section")
	if !editableSettingsSection(section) {
		fail(w, http.StatusNotFound, "SETTINGS_SECTION_NOT_FOUND", "unknown settings section")
		return
	}
	values := map[string]any{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&values); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid settings object")
		return
	}
	stripMaskedSecrets(values)
	updated, err := config.UpdateSection(s.Root, s.ConfigFile, section, values)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "SETTINGS_INVALID", err.Error())
		return
	}
	// Config is deliberately shared with the application; mutate the pointed
	// value so request handlers and subsequent static refreshes see the new
	// validated values without a process restart where that is safe.
	*s.Config = *updated
	if s.Render != nil {
		s.Render.SetMarkdownOptions(updated.Markdown.Katex, updated.Markdown.ExternalLinksNewTab, updated.Markdown.HeadingAnchors)
		prefixes := make(map[model.Locale]string, len(updated.I18n.Locales))
		for _, locale := range updated.I18n.Locales {
			if locale.Enabled {
				prefixes[model.Locale(locale.Code)] = locale.URLPrefix
			}
		}
		s.Render.SetSiteOptions(updated.Server.BaseURL, model.Locale(updated.I18n.DefaultLocale), prefixes)
		if section == "render" {
			s.Render.SetOutput(updated.Render.Output)
		}
	}
	if section == "site" || section == "i18n" || section == "markdown" || section == "search" || section == "seo" || section == "comments" {
		s.queueStaticRefresh(r.Context())
		locales := make([]model.Locale, 0, len(updated.I18n.Locales))
		for _, locale := range updated.I18n.Locales {
			if locale.Enabled {
				locales = append(locales, model.Locale(locale.Code))
			}
		}
		s.queueLocaleRefresh(r.Context(), locales...)
	}
	requiresRestart := section == "render" || section == "storage" || section == "ai" || section == "security"
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "settings.update", "config", section, map[string]any{"section": section, "requiresRestart": requiresRestart})
	ok(w, http.StatusOK, map[string]any{"section": section, "requiresRestart": requiresRestart})
}

func editableSettingsSection(section string) bool {
	switch section {
	case "site", "i18n", "render", "markdown", "storage", "ai", "search", "comments", "seo", "cache", "security", "log":
		return true
	default:
		return false
	}
}

func stripMaskedSecrets(values map[string]any) {
	for key, value := range values {
		if text, ok := value.(string); ok && text == "********" && secretSettingKey(key) {
			delete(values, key)
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			stripMaskedSecrets(nested)
		}
	}
}

func secretSettingKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "apikey") || strings.Contains(key, "api_key") || strings.Contains(key, "token")
}

type postInput struct {
	Locale      model.Locale `json:"locale"`
	BaseHash    string       `json:"baseHash"`
	Title       string       `json:"title"`
	Slug        string       `json:"slug"`
	Description string       `json:"description"`
	Body        string       `json:"body"`
	Categories  []string     `json:"categories"`
	Tags        []string     `json:"tags"`
}

func (s *Server) posts(w http.ResponseWriter, r *http.Request) {
	loc := model.Locale(r.URL.Query().Get("locale"))
	if loc == "" {
		loc = model.Locale(s.Config.I18n.SourceLocale)
	}
	items, total := s.Index.List(index.ListQuery{Type: model.ContentPost, Locale: loc, Page: parseInt(r.URL.Query().Get("page"), 1), PerPage: parseInt(r.URL.Query().Get("perPage"), 20)})
	out := make([]map[string]any, 0, len(items))
	for _, article := range items {
		version := article.Versions[loc]
		out = append(out, map[string]any{"id": article.ID, "title": version.Front.Title, "slug": version.Front.Slug, "status": version.Front.Status, "date": version.Front.Date, "locale": loc})
	}
	ok(w, http.StatusOK, map[string]any{"items": out, "total": total})
}
func (s *Server) pages(w http.ResponseWriter, r *http.Request) {
	loc := model.Locale(r.URL.Query().Get("locale"))
	if loc == "" {
		loc = model.Locale(s.Config.I18n.SourceLocale)
	}
	items, total := s.Index.List(index.ListQuery{Type: model.ContentPage, Locale: loc, Page: parseInt(r.URL.Query().Get("page"), 1), PerPage: parseInt(r.URL.Query().Get("perPage"), 20)})
	out := make([]map[string]any, 0, len(items))
	for _, article := range items {
		version := article.Versions[loc]
		out = append(out, map[string]any{"id": article.ID, "title": version.Front.Title, "slug": version.Front.Slug, "status": version.Front.Status, "date": version.Front.Date, "locale": loc})
	}
	ok(w, http.StatusOK, map[string]any{"items": out, "total": total})
}
func (s *Server) post(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, 404, "NOT_FOUND", "post not found")
		return
	}
	loc := model.Locale(r.URL.Query().Get("locale"))
	if loc == "" {
		loc = article.Source
	}
	version := article.Versions[loc]
	if version == nil {
		fail(w, 404, "LOCALE_NOT_FOUND", "post locale not found")
		return
	}
	body, err := s.Index.Body(article.ID, loc)
	if err != nil {
		fail(w, 500, "CONTENT_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"id": article.ID, "locale": loc, "front": version.Front, "body": body, "baseHash": version.BodyHash, "sourceRevision": article.SourceRev})
}
func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	s.createTyped(w, r, model.ContentPost)
}
func (s *Server) createPage(w http.ResponseWriter, r *http.Request) {
	s.createTyped(w, r, model.ContentPage)
}
func (s *Server) createTyped(w http.ResponseWriter, r *http.Request, typ model.ContentType) {
	var input postInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if input.Locale == "" {
		input.Locale = model.Locale(s.Config.I18n.SourceLocale)
		if input.Locale == "" {
			input.Locale = model.Locale(s.Config.I18n.DefaultLocale)
		}
	}
	if input.Slug == "" {
		input.Slug = slugify(input.Title)
	}
	user := r.Context().Value(userKey).(*model.User)
	front := model.FrontMatter{Title: input.Title, Slug: input.Slug, Description: input.Description, Status: model.StatusDraft, Author: user.ID, SourceLocale: input.Locale, Categories: input.Categories, Tags: input.Tags}
	article, err := s.Content.CreateBundle(typ, input.Locale, front, input.Body)
	if err != nil {
		fail(w, 422, "INVALID_POST", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticleCreated{ID: article.ID, Type: article.Type, Locale: input.Locale})
	ok(w, http.StatusCreated, map[string]any{"id": article.ID})
}
func (s *Server) revisions(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, 404, "NOT_FOUND", "post not found")
		return
	}
	loc := model.Locale(r.URL.Query().Get("locale"))
	if loc == "" {
		loc = article.Source
	}
	items, err := s.Content.ListRevisions(article.ID, loc)
	if err != nil {
		fail(w, 500, "REVISION_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) revision(w http.ResponseWriter, r *http.Request) {
	article, loc, found := s.revisionArticle(w, r)
	if !found {
		return
	}
	rev, err := strconv.Atoi(chi.URLParam(r, "rev"))
	if err != nil || rev < 1 {
		fail(w, http.StatusBadRequest, "INVALID_REVISION", "revision must be a positive number")
		return
	}
	front, body, err := s.Content.ReadRevision(article.ID, loc, rev)
	if errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "REVISION_NOT_FOUND", "revision not found")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "REVISION_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"revision": rev, "locale": loc, "front": front, "body": body})
}
func (s *Server) restoreRevision(w http.ResponseWriter, r *http.Request) {
	article, loc, found := s.revisionArticle(w, r)
	if !found {
		return
	}
	rev, err := strconv.Atoi(chi.URLParam(r, "rev"))
	if err != nil || rev < 1 {
		fail(w, http.StatusBadRequest, "INVALID_REVISION", "revision must be a positive number")
		return
	}
	front, body, err := s.Content.ReadRevision(article.ID, loc, rev)
	if errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "REVISION_NOT_FOUND", "revision not found")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "REVISION_READ_FAILED", err.Error())
		return
	}
	front.Updated = nowPtr()
	if err := s.Content.SaveVersion(article, loc, front, body, content.SaveOpts{BumpSourceRevision: loc == article.Source, Snapshot: true, MirrorAuthoritative: loc != article.Source}); err != nil {
		fail(w, http.StatusInternalServerError, "REVISION_RESTORE_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticleUpdated{ID: article.ID, Locale: loc, BodyChanged: true})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, map[string]any{"id": article.ID, "locale": loc, "restoredRevision": rev})
}
func (s *Server) revisionArticle(w http.ResponseWriter, r *http.Request) (*model.Article, model.Locale, bool) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "post not found")
		return nil, "", false
	}
	loc := model.Locale(r.URL.Query().Get("locale"))
	if loc == "" {
		loc = article.Source
	}
	if article.Versions[loc] == nil {
		fail(w, http.StatusNotFound, "LOCALE_NOT_FOUND", "post locale not found")
		return nil, "", false
	}
	return article, loc, true
}
func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, 404, "NOT_FOUND", "post not found")
		return
	}
	var input postInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if input.Locale == "" {
		input.Locale = article.Source
	}
	version := article.Versions[input.Locale]
	if version == nil {
		fail(w, 404, "LOCALE_NOT_FOUND", "post locale not found")
		return
	}
	if input.BaseHash != "" && input.BaseHash != version.BodyHash {
		fail(w, http.StatusConflict, "EDIT_CONFLICT", "this content was modified by another editor")
		return
	}
	front := version.Front
	front.Title = input.Title
	if input.Slug != "" {
		front.Slug = input.Slug
	}
	front.Description = input.Description
	if err := s.Content.SaveDraft(article.ID, input.Locale, front, input.Body); err != nil {
		fail(w, 500, "DRAFT_SAVE_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	items, err := s.Media.List(r.Context(), r.URL.Query().Get("dir"))
	if err != nil {
		fail(w, 400, "INVALID_DIRECTORY", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"path": item.Path, "name": item.Name, "url": s.Media.PublicURL(item.Path), "mime": item.MIME, "size": item.Size, "isDir": item.IsDir, "modifiedAt": item.ModTime})
	}
	ok(w, http.StatusOK, map[string]any{"items": out})
}
func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	limit, err := media.ParseByteSize(s.Config.Storage.Image.MaxUploadSize)
	if err != nil {
		limit = 20 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1024)
	if err := r.ParseMultipartForm(limit); err != nil {
		fail(w, 413, "UPLOAD_TOO_LARGE", "file exceeds upload limit")
		return
	}
	dir := r.FormValue("dir")
	files := r.MultipartForm.File["file"]
	items := make([]map[string]any, 0, len(files))
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			items = append(items, map[string]any{"ok": false, "name": header.Filename, "error": err.Error()})
			continue
		}
		head := make([]byte, 512)
		n, _ := file.Read(head)
		kind, err := media.ValidateImage(head[:n], header.Filename, s.Config.Storage.Image.AllowedTypes)
		if err == nil {
			now := time.Now()
			name := media.SanitizeName(header.Filename)
			target := filepath.ToSlash(filepath.Join(dir, fmt.Sprintf("%04d/%02d", now.Year(), now.Month()), name))
			for suffix := 2; ; suffix++ {
				if _, statErr := s.Media.Stat(r.Context(), target); errors.Is(statErr, os.ErrNotExist) {
					break
				}
				target = filepath.ToSlash(filepath.Join(dir, fmt.Sprintf("%04d/%02d", now.Year(), now.Month()), strings.TrimSuffix(name, filepath.Ext(name))+fmt.Sprintf("-%d", suffix)+filepath.Ext(name)))
			}
			err = s.Media.Put(r.Context(), target, io.MultiReader(bytes.NewReader(head[:n]), file), header.Size, kind)
			if err == nil {
				items = append(items, map[string]any{"ok": true, "path": target, "url": s.Media.PublicURL(target), "size": header.Size, "mime": kind})
			} else {
				items = append(items, map[string]any{"ok": false, "name": header.Filename, "error": err.Error()})
			}
		} else {
			items = append(items, map[string]any{"ok": false, "name": header.Filename, "error": err.Error()})
		}
		_ = file.Close()
	}
	ok(w, http.StatusCreated, map[string]any{"items": items})
}
func (s *Server) mkdirMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	path := s.Media.LocalPath(req.Dir)
	if path == "" {
		fail(w, 400, "INVALID_DIRECTORY", "invalid directory")
		return
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		fail(w, 500, "MKDIR_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	for _, path := range req.Paths {
		if err := s.Media.Delete(r.Context(), path); err != nil {
			fail(w, 400, "DELETE_FAILED", err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) updatePost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, 404, "NOT_FOUND", "post not found")
		return
	}
	var input postInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if input.Locale == "" {
		input.Locale = article.Source
	}
	version := article.Versions[input.Locale]
	if version == nil {
		fail(w, 404, "LOCALE_NOT_FOUND", "post locale not found")
		return
	}
	if input.BaseHash != "" && input.BaseHash != version.BodyHash {
		fail(w, http.StatusConflict, "EDIT_CONFLICT", "this content was modified by another editor")
		return
	}
	front := version.Front
	front.Title = input.Title
	if input.Slug != "" {
		front.Slug = input.Slug
	}
	front.Description = input.Description
	front.Categories = input.Categories
	front.Tags = input.Tags
	front.Updated = nowPtr()
	if err := s.Content.SaveVersion(article, input.Locale, front, input.Body, content.SaveOpts{MirrorAuthoritative: input.Locale != article.Source, MarkManualEdit: input.Locale != article.Source}); err != nil {
		fail(w, 500, "SAVE_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticleUpdated{ID: article.ID, Locale: input.Locale, BodyChanged: true})
	ok(w, http.StatusOK, map[string]any{"id": article.ID})
}
func (s *Server) publishPost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, 404, "NOT_FOUND", "post not found")
		return
	}
	var input struct {
		PublishAt string `json:"publishAt"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if input.PublishAt != "" {
		at, err := time.Parse(time.RFC3339, input.PublishAt)
		if err != nil || !at.After(time.Now()) {
			fail(w, http.StatusUnprocessableEntity, "INVALID_PUBLISH_TIME", "publishAt must be a future RFC3339 timestamp")
			return
		}
		if s.State == nil {
			fail(w, http.StatusServiceUnavailable, "SCHEDULER_UNAVAILABLE", "scheduled publishing is unavailable")
			return
		}
		_, err = s.State.Write().ExecContext(r.Context(), "INSERT INTO scheduled_publish(article_id,locale,publish_at,status,created_at) VALUES(?,?,?,'scheduled',?) ON CONFLICT(article_id) DO UPDATE SET locale=excluded.locale,publish_at=excluded.publish_at,status='scheduled'", string(article.ID), string(article.Source), at.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			fail(w, http.StatusInternalServerError, "SCHEDULE_FAILED", "could not schedule publication")
			return
		}
		ok(w, http.StatusAccepted, map[string]any{"id": article.ID, "scheduled": true, "publishAt": at.UTC().Format(time.RFC3339)})
		return
	}
	version := article.Versions[article.Source]
	front := version.Front
	first := front.Status != model.StatusPublished
	front.Status = model.StatusPublished
	front.Updated = nowPtr()
	if err := s.Content.SaveVersion(article, article.Source, front, version.Body, content.SaveOpts{BumpSourceRevision: true, Snapshot: true}); err != nil {
		fail(w, 500, "PUBLISH_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticlePublished{ID: article.ID, Locale: article.Source, FirstPublish: first})
	payload, _ := json.Marshal(map[string]string{"articleID": string(article.ID), "locale": string(article.Source)})
	jobID := int64(0)
	if s.Jobs != nil {
		var err error
		jobID, err = s.Jobs.Enqueue(r.Context(), jobs.Job{Kind: "render", DedupeKey: string(article.ID) + ":" + string(article.Source), Payload: payload, Priority: 10})
		if err != nil {
			fail(w, 500, "QUEUE_FAILED", err.Error())
			return
		}
	}
	translations := 0
	if s.AI != nil && s.Config.I18n.AutoTranslateOnPublish {
		for _, target := range s.translationTargets(article.Source) {
			if _, err := s.AI.Enqueue(r.Context(), article.ID, target, false); err == nil {
				translations++
			}
		}
	}
	ok(w, http.StatusAccepted, map[string]any{"id": article.ID, "queued": 1, "jobID": jobID, "translationsQueued": translations})
}
func (s *Server) translationTargets(source model.Locale) []model.Locale {
	registry, err := blogi18n.New(s.Config.I18n)
	if err != nil {
		return nil
	}
	wanted := s.Config.I18n.TranslateTargets
	if len(wanted) == 0 {
		for _, locale := range registry.Locales() {
			wanted = append(wanted, string(locale))
		}
	}
	seen := map[model.Locale]bool{}
	var out []model.Locale
	for _, value := range wanted {
		locale, ok := registry.Canonical(value)
		if !ok || locale == source || !registry.Enabled(locale) || seen[locale] {
			continue
		}
		seen[locale] = true
		out = append(out, locale)
	}
	return out
}
func (s *Server) translationTasks(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		fail(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI translation is not configured")
		return
	}
	tasks, err := s.AI.List(r.Context(), parseInt(r.URL.Query().Get("limit"), 50))
	if err != nil {
		fail(w, 500, "TASKS_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": tasks})
}
func (s *Server) createTranslationTask(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil {
		fail(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI translation is not configured")
		return
	}
	var input struct {
		ArticleID model.ArticleID `json:"articleID"`
		Target    model.Locale    `json:"targetLocale"`
		Force     bool            `json:"force"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	task, err := s.AI.Enqueue(r.Context(), input.ArticleID, input.Target, input.Force)
	if err != nil {
		if errors.Is(err, ai.ErrManualProtected) {
			fail(w, http.StatusConflict, "MANUAL_PROTECTED", err.Error())
		} else {
			fail(w, 422, "TRANSLATION_NOT_QUEUED", err.Error())
		}
		return
	}
	ok(w, http.StatusAccepted, task)
}
func (s *Server) testTranslationProvider(w http.ResponseWriter, r *http.Request) {
	if s.AI == nil || s.AI.Provider == nil {
		fail(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI translation is not configured")
		return
	}
	started := time.Now()
	if err := s.AI.Provider.Test(r.Context()); err != nil {
		fail(w, http.StatusBadGateway, "AI_TEST_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"provider": s.AI.Provider.Name(), "latencyMs": time.Since(started).Milliseconds()})
}
func (s *Server) themes(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme store is unavailable")
		return
	}
	items, err := s.Theme.List()
	if err != nil {
		fail(w, 500, "THEMES_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"active": s.Config.Theme.Active, "items": items})
}

// reloadThemes forces a fresh discovery pass. Store operations deliberately do
// not cache manifests, so this validates newly copied theme files without
// needing to restart the administrative API.
func (s *Server) reloadThemes(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme store is unavailable")
		return
	}
	items, err := s.Theme.List()
	if err != nil {
		fail(w, http.StatusInternalServerError, "THEMES_READ_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "theme.reload", "theme", "all", nil)
	ok(w, http.StatusOK, map[string]any{"active": s.Config.Theme.Active, "items": items})
}
func (s *Server) themeDetail(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme store is unavailable")
		return
	}
	name := chi.URLParam(r, "name")
	item, err := s.Theme.Get(name)
	if err != nil {
		fail(w, http.StatusNotFound, "THEME_NOT_FOUND", err.Error())
		return
	}
	values, err := s.Theme.Settings(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, "THEME_SETTINGS_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"theme": item, "values": values, "active": name == s.Config.Theme.Active})
}
func (s *Server) themeSettings(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme store is unavailable")
		return
	}
	name := chi.URLParam(r, "name")
	item, err := s.Theme.Get(name)
	if err != nil {
		fail(w, 404, "THEME_NOT_FOUND", err.Error())
		return
	}
	values, err := s.Theme.Settings(name)
	if err != nil {
		fail(w, 500, "THEME_SETTINGS_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"theme": item, "values": values})
}
func (s *Server) saveThemeSettings(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme store is unavailable")
		return
	}
	var values map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&values); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	result, warnings, err := s.Theme.SaveSettings(chi.URLParam(r, "name"), values)
	if err != nil {
		fail(w, 422, "THEME_SETTINGS_INVALID", err.Error())
		return
	}
	name := chi.URLParam(r, "name")
	if name == s.Config.Theme.Active {
		s.applyThemeSettings(r.Context(), result)
		s.refreshTheme(r.Context())
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "theme.settings.update", "theme", name, nil)
	ok(w, http.StatusOK, map[string]any{"values": result, "warnings": warnings})
}
func (s *Server) resetThemeSettings(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme store is unavailable")
		return
	}
	name := chi.URLParam(r, "name")
	values, warnings, err := s.Theme.SaveSettings(name, map[string]any{})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "THEME_SETTINGS_INVALID", err.Error())
		return
	}
	if name == s.Config.Theme.Active {
		s.applyThemeSettings(r.Context(), values)
		s.refreshTheme(r.Context())
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "theme.settings.reset", "theme", name, nil)
	ok(w, http.StatusOK, map[string]any{"values": values, "warnings": warnings})
}
func (s *Server) activateTheme(w http.ResponseWriter, r *http.Request) {
	if s.Theme == nil || s.Root == "" {
		fail(w, http.StatusServiceUnavailable, "THEMES_UNAVAILABLE", "theme configuration is unavailable")
		return
	}
	name := chi.URLParam(r, "name")
	item, err := s.Theme.Get(name)
	if err != nil {
		fail(w, http.StatusNotFound, "THEME_NOT_FOUND", err.Error())
		return
	}
	currentVersion := s.AppVersion
	if currentVersion == "" {
		currentVersion = theme.EngineVersion
	}
	if err := theme.Compatible(item.Manifest, currentVersion); err != nil {
		fail(w, http.StatusUnprocessableEntity, "THEME_INCOMPATIBLE", err.Error())
		return
	}
	updated, err := config.UpdateSection(s.Root, s.ConfigFile, "theme", map[string]any{"active": name})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "THEME_ACTIVATION_FAILED", err.Error())
		return
	}
	values, err := s.Theme.Settings(name)
	if err != nil {
		fail(w, http.StatusInternalServerError, "THEME_SETTINGS_READ_FAILED", err.Error())
		return
	}
	*s.Config = *updated
	s.applyThemeSettings(r.Context(), values)
	s.refreshTheme(r.Context())
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "theme.activate", "theme", name, nil)
	ok(w, http.StatusOK, map[string]any{"active": name, "values": values})
}
func (s *Server) applyThemeSettings(_ context.Context, values map[string]any) {
	if s.Render != nil {
		s.Render.SetThemeSettings(values)
	}
}
func (s *Server) refreshTheme(ctx context.Context) {
	s.queueStaticRefresh(ctx)
	locales := make([]model.Locale, 0, len(s.Config.I18n.Locales))
	for _, locale := range s.Config.I18n.Locales {
		if locale.Enabled {
			locales = append(locales, model.Locale(locale.Code))
		}
	}
	s.queueLocaleRefresh(ctx, locales...)
}
func (s *Server) backups(w http.ResponseWriter, r *http.Request) {
	items, err := backup.List(s.Backup.OutputDir)
	if err != nil {
		fail(w, 500, "BACKUPS_READ_FAILED", err.Error())
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IncludeMedia   *bool `json:"includeMedia"`
		ExcludeSecrets bool  `json:"excludeSecrets"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	opts := s.Backup
	if input.IncludeMedia != nil {
		opts.IncludeMedia = *input.IncludeMedia
	}
	opts.ExcludeSecrets = input.ExcludeSecrets
	item, err := backup.Create(opts)
	if err != nil {
		fail(w, 500, "BACKUP_FAILED", err.Error())
		return
	}
	ok(w, http.StatusCreated, item)
}
func (s *Server) backupPath(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return "", errors.New("invalid backup name")
	}
	return filepath.Join(s.Backup.OutputDir, name), nil
}
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	path, err := s.backupPath(chi.URLParam(r, "name"))
	if err != nil {
		fail(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "backup was not found")
		return
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "backup was not found")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "BACKUP_READ_FAILED", err.Error())
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, "BACKUP_READ_FAILED", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}
func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	path, err := s.backupPath(name)
	if err != nil {
		fail(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "backup was not found")
		return
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "backup was not found")
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, "BACKUP_DELETE_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "backup.delete", "backup", name, nil)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string `json:"name"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if !input.Confirm {
		fail(w, http.StatusUnprocessableEntity, "RESTORE_CONFIRMATION_REQUIRED", "set confirm to true to restore a backup")
		return
	}
	path, err := s.backupPath(input.Name)
	if err != nil {
		fail(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "backup was not found")
		return
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "backup was not found")
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, "BACKUP_READ_FAILED", err.Error())
		return
	}
	if err := backup.Restore(backup.RestoreOptions{Root: s.Root, Archive: path, Confirm: true}); err != nil {
		fail(w, http.StatusInternalServerError, "BACKUP_RESTORE_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "backup.restore", "backup", input.Name, nil)
	ok(w, http.StatusOK, map[string]any{"restored": input.Name, "restartRecommended": true})
}

type importJobPayload struct {
	Name                  string           `json:"name"`
	Locale                model.Locale     `json:"locale"`
	Status                model.Status     `json:"status,omitempty"`
	DryRun                bool             `json:"dryRun"`
	CreateMissingTaxonomy bool             `json:"createMissingTaxonomy"`
	Phase                 string           `json:"phase"`
	Report                *importer.Report `json:"report,omitempty"`
	Error                 string           `json:"error,omitempty"`
}

func (s *Server) importLocale(value string) (model.Locale, bool) {
	if value == "" {
		return model.Locale(s.Config.I18n.SourceLocale), true
	}
	for _, configured := range s.Config.I18n.Locales {
		if configured.Code == value && configured.Enabled {
			return model.Locale(value), true
		}
	}
	return "", false
}

func (s *Server) createImport(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		fail(w, http.StatusServiceUnavailable, "IMPORT_UNAVAILABLE", "task queue is unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "IMPORT_TOO_LARGE", "import upload must be at most 100 MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "IMPORT_FILE_REQUIRED", "multipart field file is required")
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	if name == "." || (!strings.EqualFold(filepath.Ext(name), ".md") && !strings.EqualFold(filepath.Ext(name), ".zip")) {
		fail(w, http.StatusUnprocessableEntity, "IMPORT_TYPE_INVALID", "upload a Markdown or ZIP file")
		return
	}
	locale, valid := s.importLocale(r.FormValue("defaultLocale"))
	if !valid {
		fail(w, http.StatusUnprocessableEntity, "IMPORT_LOCALE_INVALID", "defaultLocale is not an enabled locale")
		return
	}
	status := model.Status(r.FormValue("defaultStatus"))
	if status != "" && status != model.StatusDraft && status != model.StatusPublished {
		fail(w, http.StatusUnprocessableEntity, "IMPORT_STATUS_INVALID", "defaultStatus must be draft or published")
		return
	}
	dryRun := r.FormValue("dryRun") == "true"
	createMissing := r.FormValue("createMissingTaxonomy") != "false"
	payload := importJobPayload{Name: name, Locale: locale, Status: status, DryRun: dryRun, CreateMissingTaxonomy: createMissing, Phase: "queued"}
	dir := filepath.Join(s.Root, "data", "imports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, http.StatusInternalServerError, "IMPORT_STAGING_FAILED", "could not stage import")
		return
	}
	target, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		fail(w, http.StatusInternalServerError, "IMPORT_STAGING_FAILED", "could not stage import")
		return
	}
	path := target.Name()
	if err := target.Chmod(0o600); err != nil {
		_ = target.Close()
		_ = os.Remove(path)
		fail(w, http.StatusInternalServerError, "IMPORT_STAGING_FAILED", "could not stage import")
		return
	}
	_, copyErr := io.Copy(target, file)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		fail(w, http.StatusInternalServerError, "IMPORT_STAGING_FAILED", "could not stage import")
		return
	}
	encoded, _ := json.Marshal(payload)
	id, err := s.Jobs.Enqueue(r.Context(), jobs.Job{Kind: "import", Payload: encoded, MaxAttempts: 1})
	if err != nil {
		_ = os.Remove(path)
		fail(w, http.StatusInternalServerError, "IMPORT_QUEUE_FAILED", "could not queue import")
		return
	}
	actor := r.Context().Value(userKey).(*model.User).Username
	s.audit(r, actor, "import.create", "import", fmt.Sprint(id), map[string]any{"name": name, "dryRun": dryRun})
	go s.runImport(id, path, payload)
	ok(w, http.StatusAccepted, map[string]any{"jobId": id, "phase": "queued"})
}

func (s *Server) runImport(id int64, path string, payload importJobPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	defer os.Remove(path)
	started, err := s.Jobs.Start(ctx, id, fmt.Sprintf("import-%d", id))
	if err != nil || !started {
		return
	}
	payload.Phase = "parsing"
	_ = s.Jobs.UpdatePayload(ctx, id, payload)
	files, err := importer.ReadPath(path)
	if err == nil {
		payload.Phase = "writing"
		_ = s.Jobs.UpdatePayload(ctx, id, payload)
		var report importer.Report
		report, err = (importer.Service{Content: s.Content, Index: s.Index, Taxonomy: s.Taxonomy}).Run(ctx, files, importer.Options{Locale: payload.Locale, Status: payload.Status, DryRun: payload.DryRun, CreateMissingTaxonomy: payload.CreateMissingTaxonomy})
		payload.Report = &report
	}
	payload.Phase = "done"
	if err != nil {
		payload.Error = err.Error()
		_ = s.Jobs.UpdatePayload(ctx, id, payload)
		_ = s.Jobs.Fail(ctx, id, err)
		return
	}
	_ = s.Jobs.UpdatePayload(ctx, id, payload)
	_ = s.Jobs.Complete(ctx, id)
}

func (s *Server) importStatus(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		fail(w, http.StatusServiceUnavailable, "IMPORT_UNAVAILABLE", "task queue is unavailable")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusNotFound, "IMPORT_NOT_FOUND", "import job was not found")
		return
	}
	job, err := s.Jobs.Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || job.Kind != "import" {
		fail(w, http.StatusNotFound, "IMPORT_NOT_FOUND", "import job was not found")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "IMPORT_READ_FAILED", "could not read import job")
		return
	}
	var payload importJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		fail(w, http.StatusInternalServerError, "IMPORT_READ_FAILED", "could not read import report")
		return
	}
	if job.Status == "running" {
		payload.Phase = "writing"
	}
	if job.Status == "failed" && payload.Error == "" {
		payload.Error = job.LastError
	}
	ok(w, http.StatusOK, map[string]any{"jobId": job.ID, "status": job.Status, "createdAt": job.CreatedAt, "updatedAt": job.UpdatedAt, "phase": payload.Phase, "report": payload.Report, "error": payload.Error})
}
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	items := make([]map[string]any, 0, len(s.Users.List()))
	for _, user := range s.Users.List() {
		items = append(items, publicUser(user))
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct{ Username, Email, Password, Role, Locale string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if !validRole(input.Role) {
		fail(w, 422, "INVALID_ROLE", "invalid user role")
		return
	}
	if input.Locale == "" {
		input.Locale = s.Config.I18n.DefaultLocale
	}
	user, err := s.Users.Create(input.Username, input.Email, input.Password, input.Role, input.Locale)
	if err != nil {
		fail(w, 422, "USER_CREATE_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "user.create", "user", user.ID, map[string]any{"role": user.Role})
	ok(w, http.StatusCreated, publicUser(user))
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	user, exists := s.Users.ByID(chi.URLParam(r, "id"))
	if !exists {
		fail(w, 404, "NOT_FOUND", "user not found")
		return
	}
	var input struct {
		Email, DisplayName, Avatar, Role, Locale string
		Disabled                                 *bool
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	copy := *user
	if input.Email != "" {
		copy.Email = input.Email
	}
	if input.DisplayName != "" {
		copy.DisplayName = input.DisplayName
	}
	if input.Avatar != "" {
		copy.Avatar = input.Avatar
	}
	if input.Locale != "" {
		copy.Locale = model.Locale(input.Locale)
	}
	if input.Role != "" {
		if !validRole(input.Role) {
			fail(w, 422, "INVALID_ROLE", "invalid user role")
			return
		}
		copy.Role = input.Role
	}
	if input.Disabled != nil {
		copy.Disabled = *input.Disabled
		copy.TokenVersion++
	}
	if wouldRemoveLastAdmin(s.Users, user, &copy) {
		fail(w, http.StatusUnprocessableEntity, "LAST_ADMIN", "at least one active administrator is required")
		return
	}
	if err := s.Users.Save(&copy); err != nil {
		fail(w, 500, "USER_UPDATE_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "user.update", "user", copy.ID, map[string]any{"role": copy.Role, "disabled": copy.Disabled})
	ok(w, http.StatusOK, publicUser(&copy))
}
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	user, exists := s.Users.ByID(chi.URLParam(r, "id"))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}
	if user.Role == "admin" && !user.Disabled && activeAdminCount(s.Users) <= 1 {
		fail(w, http.StatusUnprocessableEntity, "LAST_ADMIN", "at least one active administrator is required")
		return
	}
	if err := s.Users.Delete(user.ID); err != nil {
		fail(w, http.StatusInternalServerError, "USER_DELETE_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "user.delete", "user", user.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}
func activeAdminCount(users *auth.Users) int {
	count := 0
	for _, candidate := range users.List() {
		if candidate.Role == "admin" && !candidate.Disabled {
			count++
		}
	}
	return count
}
func wouldRemoveLastAdmin(users *auth.Users, before, after *model.User) bool {
	return before.Role == "admin" && !before.Disabled && (after.Role != "admin" || after.Disabled) && activeAdminCount(users) <= 1
}
func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	user, exists := s.Users.ByID(chi.URLParam(r, "id"))
	if !exists {
		fail(w, 404, "NOT_FOUND", "user not found")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if err := s.Users.ResetPassword(user.Username, input.Password); err != nil {
		fail(w, 422, "PASSWORD_RESET_FAILED", err.Error())
		return
	}
	s.audit(r, r.Context().Value(userKey).(*model.User).Username, "user.password.reset", "user", user.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}
func validRole(role string) bool {
	return role == "admin" || role == "editor" || role == "author" || role == "translator"
}
func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, 404, "NOT_FOUND", "post not found")
		return
	}
	locales := s.removePublishedUnits(article)
	if err := s.setArticleStatus(article, model.StatusTrashed); err != nil {
		fail(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticleDeleted{ID: article.ID, Type: article.Type})
	s.queueLocaleRefresh(r.Context(), locales...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unpublishPost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "post not found")
		return
	}
	locales := s.removePublishedUnits(article)
	if err := s.setArticleStatus(article, model.StatusDraft); err != nil {
		fail(w, http.StatusInternalServerError, "UNPUBLISH_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticleUpdated{ID: article.ID, Locale: article.Source, BodyChanged: false})
	s.queueLocaleRefresh(r.Context(), locales...)
	ok(w, http.StatusOK, map[string]any{"id": article.ID, "status": model.StatusDraft})
}

func (s *Server) restorePost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "post not found")
		return
	}
	if article.Versions[article.Source] == nil || article.Versions[article.Source].Front.Status != model.StatusTrashed {
		fail(w, http.StatusConflict, "NOT_TRASHED", "only trashed content can be restored")
		return
	}
	if err := s.setArticleStatus(article, model.StatusDraft); err != nil {
		fail(w, http.StatusInternalServerError, "RESTORE_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(article)
	s.Events.Publish(r.Context(), events.ArticleUpdated{ID: article.ID, Locale: article.Source, BodyChanged: false})
	ok(w, http.StatusOK, map[string]any{"id": article.ID, "status": model.StatusDraft})
}

func (s *Server) duplicatePost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists || article.Versions[article.Source] == nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "post not found")
		return
	}
	version := article.Versions[article.Source]
	front := version.Front
	front.ID = ""
	front.Title = "Copy of " + front.Title
	front.Status = model.StatusDraft
	front.Updated = nowPtr()
	front.Slug = s.nextCopySlug(article.Type, article.Source, front.Slug)
	copy, err := s.Content.CreateBundle(article.Type, article.Source, front, version.Body)
	if err != nil {
		fail(w, http.StatusInternalServerError, "DUPLICATE_FAILED", err.Error())
		return
	}
	s.Index.UpsertArticle(copy)
	s.Events.Publish(r.Context(), events.ArticleCreated{ID: copy.ID, Type: copy.Type, Locale: copy.Source})
	ok(w, http.StatusCreated, map[string]any{"id": copy.ID})
}

func (s *Server) purgePost(w http.ResponseWriter, r *http.Request) {
	article, exists := s.Index.Article(model.ArticleID(chi.URLParam(r, "id")))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "post not found")
		return
	}
	if article.Versions[article.Source] == nil || article.Versions[article.Source].Front.Status != model.StatusTrashed {
		fail(w, http.StatusConflict, "PURGE_REQUIRES_TRASH", "content must be moved to trash before permanent deletion")
		return
	}
	locales := s.removePublishedUnits(article)
	for locale := range article.Versions {
		locales = append(locales, locale)
	}
	if err := s.Content.DeleteBundle(article); err != nil {
		fail(w, http.StatusInternalServerError, "PURGE_FAILED", err.Error())
		return
	}
	s.Index.RemoveArticle(article.ID)
	s.Events.Publish(r.Context(), events.ArticleDeleted{ID: article.ID, Type: article.Type})
	s.queueLocaleRefresh(r.Context(), locales...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setArticleStatus(article *model.Article, status model.Status) error {
	source := article.Versions[article.Source]
	if source == nil {
		return errors.New("source locale version is missing")
	}
	now := nowPtr()
	front := source.Front
	front.Status, front.Updated = status, now
	if err := s.Content.SaveVersion(article, article.Source, front, source.Body, content.SaveOpts{BumpSourceRevision: true, Snapshot: true}); err != nil {
		return err
	}
	locales := make([]model.Locale, 0, len(article.Versions))
	for locale := range article.Versions {
		if locale != article.Source {
			locales = append(locales, locale)
		}
	}
	sort.Slice(locales, func(i, j int) bool { return locales[i] < locales[j] })
	for _, locale := range locales {
		version := article.Versions[locale]
		if version == nil {
			continue
		}
		derived := version.Front
		derived.Status, derived.Updated = status, now
		if err := s.Content.SaveVersion(article, locale, derived, version.Body, content.SaveOpts{MirrorAuthoritative: true}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) removePublishedUnits(article *model.Article) []model.Locale {
	locales := make([]model.Locale, 0, len(article.Versions))
	for locale, version := range article.Versions {
		if version == nil || version.Front.Status != model.StatusPublished {
			continue
		}
		if s.Render != nil {
			_ = s.Render.Remove(article, locale)
		}
		locales = append(locales, locale)
	}
	return locales
}

func (s *Server) queueLocaleRefresh(ctx context.Context, locales ...model.Locale) {
	if s.Jobs == nil {
		return
	}
	seen := map[model.Locale]bool{}
	for _, locale := range locales {
		if locale == "" || seen[locale] {
			continue
		}
		seen[locale] = true
		payload, _ := json.Marshal(map[string]string{"locale": string(locale)})
		_, _ = s.Jobs.Enqueue(ctx, jobs.Job{Kind: "render", DedupeKey: "site-refresh:" + string(locale), Payload: payload, Priority: 20})
	}
}

func (s *Server) nextCopySlug(kind model.ContentType, locale model.Locale, slug string) string {
	base := strings.Trim(slug, "-")
	if base == "" {
		base = "copy"
	}
	for n := 1; ; n++ {
		candidate := base + "-copy"
		if n > 1 {
			candidate += "-" + strconv.Itoa(n)
		}
		used := false
		for _, existing := range s.Index.Articles() {
			if existing.Type == kind && existing.Versions[locale] != nil && existing.Versions[locale].Front.Slug == candidate {
				used = true
				break
			}
		}
		if !used {
			return candidate
		}
	}
}
func (s *Server) perm(action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := r.Context().Value(userKey).(*model.User)
			if !auth.Can(user.Role, action) {
				fail(w, 403, "FORBIDDEN", "missing permission "+action)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
func parseInt(raw string, fallback int) int {
	var n int
	if _, err := fmt.Sscan(raw, &n); err != nil || n < 1 {
		return fallback
	}
	return n
}
func slugify(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	dash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteRune('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "post"
	}
	return out
}
func nowPtr() *time.Time { now := time.Now(); return &now }
func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	result := s.Taxonomy.LoadAll()
	items := make([]*model.Category, 0, len(result.Categories))
	for _, item := range result.Categories {
		items = append(items, item)
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) saveCategory(w http.ResponseWriter, r *http.Request) {
	var value model.Category
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		value.ID = id
	}
	if value.ID == "" {
		value.ID = slugify(value.Slug)
	}
	if err := s.Taxonomy.SaveCategory(&value); err != nil {
		fail(w, 422, "INVALID_CATEGORY", err.Error())
		return
	}
	if err := s.Index.ReloadTaxonomy(s.Taxonomy); err != nil {
		fail(w, 500, "INDEX_RELOAD_FAILED", err.Error())
		return
	}
	s.Events.Publish(r.Context(), events.CategoryChanged{ID: value.ID, Op: "updated"})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, value)
}
func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, exists := s.Index.Category(id); !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "category not found")
		return
	}
	if references, total := s.Index.List(index.ListQuery{Category: id}); total > 0 {
		migrateTo := r.URL.Query().Get("migrateTo")
		if migrateTo != "" {
			if migrateTo == id {
				fail(w, http.StatusUnprocessableEntity, "INVALID_MIGRATION", "migrateTo must be a different category")
				return
			}
			if _, exists := s.Index.Category(migrateTo); !exists {
				fail(w, http.StatusUnprocessableEntity, "INVALID_MIGRATION", "migration destination category was not found")
				return
			}
			changed := 0
			for _, article := range references {
				source := article.Versions[article.Source]
				if source == nil || !containsString(source.Front.Categories, id) {
					continue
				}
				front := source.Front
				front.Categories = replaceString(front.Categories, id, migrateTo)
				front.Updated = nowPtr()
				if err := s.Content.SaveVersion(article, article.Source, front, source.Body, content.SaveOpts{BumpSourceRevision: true, Snapshot: true}); err != nil {
					fail(w, http.StatusInternalServerError, "CATEGORY_MIGRATION_FAILED", err.Error())
					return
				}
				for locale, version := range article.Versions {
					if locale == article.Source {
						continue
					}
					if err := s.Content.SaveVersion(article, locale, version.Front, version.Body, content.SaveOpts{}); err != nil {
						fail(w, http.StatusInternalServerError, "CATEGORY_MIGRATION_FAILED", err.Error())
						return
					}
				}
				s.Index.UpsertArticle(article)
				changed++
			}
			if err := s.Taxonomy.Delete("categories", id); err != nil {
				fail(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
				return
			}
			_ = s.Index.ReloadTaxonomy(s.Taxonomy)
			s.Events.Publish(r.Context(), events.CategoryChanged{ID: id, Op: "deleted"})
			s.queueStaticRefresh(r.Context())
			ok(w, http.StatusOK, map[string]any{"removed": id, "migratedTo": migrateTo, "articlesUpdated": changed})
			return
		}
		ids := make([]string, 0, len(references))
		for _, article := range references {
			ids = append(ids, string(article.ID))
		}
		failWithData(w, http.StatusConflict, "CATEGORY_IN_USE", "category is referenced by published or draft content", map[string]any{"references": total, "articleIDs": ids})
		return
	}
	if err := s.Taxonomy.Delete("categories", id); err != nil {
		fail(w, 404, "NOT_FOUND", err.Error())
		return
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.CategoryChanged{ID: id, Op: "deleted"})
	s.queueStaticRefresh(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	result := s.Taxonomy.LoadAll()
	items := make([]*model.Tag, 0, len(result.Tags))
	for _, item := range result.Tags {
		items = append(items, item)
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) saveTag(w http.ResponseWriter, r *http.Request) {
	var value model.Tag
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		fail(w, 400, "INVALID_JSON", "invalid request body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		value.ID = id
	}
	if value.ID == "" {
		value.ID = slugify(value.Slug)
	}
	if err := s.Taxonomy.SaveTag(&value); err != nil {
		fail(w, 422, "INVALID_TAG", err.Error())
		return
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.TagChanged{ID: value.ID, Op: "updated"})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, value)
}
func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, exists := s.Index.Tag(id); !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "tag not found")
		return
	}
	changed := 0
	for _, article := range s.Index.Articles() {
		source := article.Versions[article.Source]
		if source == nil || !containsString(source.Front.Tags, id) {
			continue
		}
		front := source.Front
		front.Tags = withoutString(front.Tags, id)
		front.Updated = nowPtr()
		if err := s.Content.SaveVersion(article, article.Source, front, source.Body, content.SaveOpts{BumpSourceRevision: true, Snapshot: true}); err != nil {
			fail(w, http.StatusInternalServerError, "TAG_MIGRATION_FAILED", err.Error())
			return
		}
		for locale, version := range article.Versions {
			if locale == article.Source {
				continue
			}
			if err := s.Content.SaveVersion(article, locale, version.Front, version.Body, content.SaveOpts{}); err != nil {
				fail(w, http.StatusInternalServerError, "TAG_MIGRATION_FAILED", err.Error())
				return
			}
		}
		s.Index.UpsertArticle(article)
		changed++
	}
	if err := s.Taxonomy.Delete("tags", id); err != nil {
		fail(w, 404, "NOT_FOUND", err.Error())
		return
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.TagChanged{ID: id, Op: "deleted"})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, map[string]any{"removed": id, "articlesUpdated": changed})
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func withoutString(values []string, unwanted string) []string {
	out := values[:0]
	for _, value := range values {
		if value != unwanted {
			out = append(out, value)
		}
	}
	return out
}
func replaceString(values []string, old, replacement string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == old {
			value = replacement
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
func (s *Server) links(w http.ResponseWriter, r *http.Request) {
	ok(w, http.StatusOK, map[string]any{"items": s.Index.Links(), "groups": s.Index.LinkGroups()})
}
func (s *Server) linkGroups(w http.ResponseWriter, r *http.Request) {
	ok(w, http.StatusOK, map[string]any{"items": s.Index.LinkGroups()})
}
func (s *Server) link(w http.ResponseWriter, r *http.Request) {
	item, exists := s.Index.Link(chi.URLParam(r, "id"))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "link not found")
		return
	}
	ok(w, http.StatusOK, item)
}
func (s *Server) saveLink(w http.ResponseWriter, r *http.Request) {
	var value model.Link
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&value); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		value.ID = id
	}
	if value.ID == "" {
		value.ID = slugify(value.Name)
	}
	if value.URL == "" || (value.Status != "" && value.Status != "active" && value.Status != "pending" && value.Status != "rejected") {
		fail(w, http.StatusUnprocessableEntity, "INVALID_LINK", "link URL or status is invalid")
		return
	}
	if value.Status == "" {
		value.Status = "active"
	}
	if err := s.Taxonomy.SaveLink(&value); err != nil {
		fail(w, http.StatusUnprocessableEntity, "INVALID_LINK", err.Error())
		return
	}
	if err := s.Index.ReloadTaxonomy(s.Taxonomy); err != nil {
		fail(w, http.StatusInternalServerError, "INDEX_RELOAD_FAILED", err.Error())
		return
	}
	s.Events.Publish(r.Context(), events.LinkChanged{ID: value.ID, Op: "updated"})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, value)
}
func (s *Server) saveLinkGroups(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Groups []*model.LinkGroup `json:"groups"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if err := s.Taxonomy.SaveLinkGroups(input.Groups); err != nil {
		fail(w, http.StatusUnprocessableEntity, "INVALID_GROUPS", err.Error())
		return
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.LinkChanged{ID: "groups", Op: "updated"})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, map[string]any{"items": s.Index.LinkGroups()})
}
func (s *Server) reorderLinks(w http.ResponseWriter, r *http.Request) {
	var input []struct {
		ID    string `json:"id"`
		Group string `json:"group"`
		Order int    `json:"order"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	for _, update := range input {
		item, exists := s.Index.Link(update.ID)
		if !exists {
			fail(w, http.StatusNotFound, "NOT_FOUND", "link not found: "+update.ID)
			return
		}
		item.Group, item.Order = update.Group, update.Order
		if err := s.Taxonomy.SaveLink(item); err != nil {
			fail(w, http.StatusInternalServerError, "LINK_REORDER_FAILED", err.Error())
			return
		}
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.LinkChanged{ID: "", Op: "reordered"})
	s.queueStaticRefresh(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Taxonomy.Delete("links", id); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.LinkChanged{ID: id, Op: "deleted"})
	s.queueStaticRefresh(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) menus(w http.ResponseWriter, r *http.Request) {
	ok(w, http.StatusOK, map[string]any{"items": s.Index.Menus()})
}
func (s *Server) menu(w http.ResponseWriter, r *http.Request) {
	item, exists := s.Index.Menu(chi.URLParam(r, "id"))
	if !exists {
		fail(w, http.StatusNotFound, "NOT_FOUND", "menu not found")
		return
	}
	ok(w, http.StatusOK, item)
}
func (s *Server) saveMenu(w http.ResponseWriter, r *http.Request) {
	var value model.Menu
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&value); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		value.ID = id
	}
	if value.ID == "" {
		fail(w, http.StatusUnprocessableEntity, "INVALID_MENU", "menu id is required")
		return
	}
	if err := s.Taxonomy.SaveMenu(&value); err != nil {
		fail(w, http.StatusUnprocessableEntity, "INVALID_MENU", err.Error())
		return
	}
	if err := s.Index.ReloadTaxonomy(s.Taxonomy); err != nil {
		fail(w, http.StatusInternalServerError, "INDEX_RELOAD_FAILED", err.Error())
		return
	}
	s.Events.Publish(r.Context(), events.MenuChanged{ID: value.ID})
	s.queueStaticRefresh(r.Context())
	ok(w, http.StatusOK, value)
}
func (s *Server) deleteMenu(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Taxonomy.Delete("menus", id); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	_ = s.Index.ReloadTaxonomy(s.Taxonomy)
	s.Events.Publish(r.Context(), events.MenuChanged{ID: id})
	s.queueStaticRefresh(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) menuTargets(w http.ResponseWriter, r *http.Request) {
	typeName := r.URL.Query().Get("type")
	typ := model.ContentPost
	if typeName == "page" {
		typ = model.ContentPage
	}
	locale := model.Locale(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = model.Locale(s.Config.I18n.SourceLocale)
	}
	items, _ := s.Index.List(index.ListQuery{Type: typ, Locale: locale, Search: r.URL.Query().Get("q"), Page: 1, PerPage: 30})
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		version := item.Versions[locale]
		out = append(out, map[string]any{"id": item.ID, "title": version.Front.Title, "slug": version.Front.Slug, "type": typ})
	}
	ok(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) queueStaticRefresh(ctx context.Context) {
	if s.Jobs == nil {
		return
	}
	for _, article := range s.Index.Articles() {
		for locale, version := range article.Versions {
			if version.Front.Status != model.StatusPublished {
				continue
			}
			payload, _ := json.Marshal(map[string]string{"articleID": string(article.ID), "locale": string(locale)})
			_, _ = s.Jobs.Enqueue(ctx, jobs.Job{Kind: "render", DedupeKey: string(article.ID) + ":" + string(locale), Payload: payload, Priority: 20})
		}
	}
}
func (s *Server) setSession(w http.ResponseWriter, r *http.Request, user *model.User) {
	token, _ := auth.Sign(s.Config.Security.SessionSecret, auth.NewClaims(user, time.Duration(s.Config.Security.SessionMaxAge)*time.Second))
	http.SetCookie(w, &http.Cookie{Name: "blog_session", Value: token, Path: "/", MaxAge: s.Config.Security.SessionMaxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cookieSecure(r)})
}
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("blog_session")
		if err != nil {
			fail(w, 401, "UNAUTHORIZED", "authentication required")
			return
		}
		claims, err := auth.Parse(s.Config.Security.SessionSecret, cookie.Value)
		if err != nil {
			fail(w, 401, "UNAUTHORIZED", "invalid session")
			return
		}
		user, ok := s.Users.ByID(claims.UID)
		if !ok || user.Disabled || user.TokenVersion != claims.TokenVersion {
			fail(w, 401, "UNAUTHORIZED", "session is no longer valid")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/auth/logout" {
			if !s.sameOrigin(r) {
				fail(w, 403, "ORIGIN_INVALID", "request origin does not match site URL")
				return
			}
			csrf, err := r.Cookie("csrf_token")
			if err != nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
				fail(w, 403, "CSRF_INVALID", "missing or invalid CSRF token")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}
func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	want, err := url.Parse(s.Config.Server.BaseURL)
	if err != nil {
		return false
	}
	got, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return got.Scheme == want.Scheme && got.Host == want.Host
}
func (s *Server) authMiddleware(next http.Handler) http.Handler { return s.requireAuth(next.ServeHTTP) }
func (s *Server) cookieSecure(r *http.Request) bool {
	if s.Config.Security.CookieSecure == "true" {
		return true
	}
	if s.Config.Security.CookieSecure != "auto" {
		return false
	}
	if r != nil && r.TLS != nil {
		return true
	}
	if r != nil && s.requestFromTrustedProxy(r) {
		return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
	}
	// "auto" follows the transport of the request that is setting the cookie.
	// A configured HTTPS canonical URL must not make a direct HTTP first-run
	// session unusable; the browser would discard that Secure cookie.
	return false
}
func publicUser(user *model.User) map[string]any {
	return map[string]any{"id": user.ID, "username": user.Username, "email": user.Email, "displayName": user.DisplayName, "role": user.Role, "locale": user.Locale, "disabled": user.Disabled}
}
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := make([]byte, 12)
		_, _ = rand.Read(id)
		w.Header().Set("X-Request-ID", base64.RawURLEncoding.EncodeToString(id))
		next.ServeHTTP(w, r)
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}
