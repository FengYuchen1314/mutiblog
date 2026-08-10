package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

const sessionCookieName = "mutiblog_session"

type Options struct {
	Repository *fsrepo.Repository
	ConsoleDir string
	Version    string
	Logger     *slog.Logger
}

type Server struct {
	repository *fsrepo.Repository
	consoleDir string
	version    string
	logger     *slog.Logger
	sessions   *auth.SessionStore
	mux        *http.ServeMux
	setupMu    sync.Mutex
}

func New(options Options) (*Server, error) {
	if options.Repository == nil {
		return nil, errors.New("repository is required")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	server := &Server{
		repository: options.Repository,
		consoleDir: options.ConsoleDir,
		version:    options.Version,
		logger:     options.Logger,
		sessions:   auth.NewSessionStore(24 * time.Hour),
		mux:        http.NewServeMux(),
	}
	server.routes()
	return server, nil
}

func (s *Server) Handler() http.Handler {
	return s.recoverPanic(s.requestLog(s.securityHeaders(s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health/live", s.handleLive)
	s.mux.HandleFunc("GET /health/ready", s.handleReady)
	s.mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireSession(s.handleLogout))
	s.mux.HandleFunc("GET /api/v1/auth/session", s.requireSession(s.handleSession))
	s.mux.HandleFunc("GET /api/v1/admin/system/status", s.requireSession(s.handleSystemStatus))
	s.mux.HandleFunc("GET /console/", s.handleConsole)
	s.mux.HandleFunc("GET /", s.handlePublic)
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	probe := filepath.Join(s.repository.Root(), "state", ".ready")
	if err := os.WriteFile(probe, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "repository_unavailable", "The data repository is not writable.", nil)
		return
	}
	_ = os.Remove(probe)
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, _ *http.Request) {
	initialized, _ := s.repository.Exists("config/site.yaml")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"initialized": initialized,
		"version":     s.version,
		"dataDir":     s.repository.Root(),
		"index":       "pending",
		"publisher":   "idle",
	})
}

func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if s.consoleDir == "" {
		http.NotFound(w, r)
		return
	}
	clean := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
	clean = strings.TrimPrefix(clean, "console/")
	if clean == "." || clean == "" {
		clean = "index.html"
	}
	requested := filepath.Join(s.consoleDir, clean)
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		s.serveFile(w, requested)
		return
	}
	s.serveFile(w, filepath.Join(s.consoleDir, "index.html"))
}

func (s *Server) handlePublic(w http.ResponseWriter, r *http.Request) {
	current := filepath.Join(s.repository.Root(), "generated", "current")
	clean := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
	if clean == "." || clean == "" {
		clean = "index.html"
	}
	requested := filepath.Join(current, clean)
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		s.serveFile(w, requested)
		return
	}
	if info, err := os.Stat(filepath.Join(requested, "index.html")); err == nil && !info.IsDir() {
		s.serveFile(w, filepath.Join(requested, "index.html"))
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serveFile(w http.ResponseWriter, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	s.writeJSON(w, status, map[string]any{"code": code, "message": message, "fields": fields})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	return true
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "durationMs", time.Since(started).Milliseconds())
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "error", fmt.Sprint(recovered), "path", r.URL.Path)
				s.writeError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
