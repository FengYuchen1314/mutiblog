package server

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

type sessionContextKey struct{}

type sessionContext struct {
	Token   string
	Session auth.Session
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.repository.Exists("config/initialized")
	if err != nil || !initialized {
		s.writeError(w, http.StatusPreconditionRequired, "setup_required", "MutiBlog must be initialized first.", nil)
		return
	}
	var request loginRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	loginKey := clientIP(r)
	if allowed, retry := s.logins.Allow(loginKey); !allowed {
		s.recordSecurityEvent(r, "login", "rate-limited", request.Username)
		seconds := max(1, int(retry.Round(time.Second)/time.Second))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		s.writeError(w, http.StatusTooManyRequests, "login_rate_limited", "Too many login attempts. Please try again later.", nil)
		return
	}
	// Password changes use the same lock through the hash write and session
	// invalidation. Keep credential verification and session creation in one
	// critical section so an old-password login cannot create a new session
	// immediately after the password-change request clears existing sessions.
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var admin domain.AdminConfig
	if err := s.repository.ReadYAML("config/admin.yaml", &admin); err != nil {
		s.logger.Error("read administrator failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "authentication_unavailable", "Authentication is temporarily unavailable.", nil)
		return
	}
	usernameMatches := subtle.ConstantTimeCompare([]byte(strings.ToLower(request.Username)), []byte(admin.Username)) == 1
	passwordMatches, verifyErr := auth.VerifyPassword(request.Password, admin.PasswordHash)
	if verifyErr != nil {
		s.logger.Error("verify administrator password failed", "error", verifyErr)
	}
	if !usernameMatches || verifyErr != nil || !passwordMatches {
		s.logins.Failed(loginKey)
		s.recordSecurityEvent(r, "login", "failed", request.Username)
		time.Sleep(250 * time.Millisecond)
		s.writeError(w, http.StatusUnauthorized, "invalid_credentials", "The username or password is incorrect.", nil)
		return
	}
	s.logins.Reset(loginKey)
	token, session, err := s.sessions.Create(admin.Username)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "session_failed", "Cannot create a session.", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
	})
	s.recordSecurityEvent(r, "login", "succeeded", admin.Username)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"username":    admin.Username,
		"csrfToken":   session.CSRFToken,
		"expiresAt":   session.ExpiresAt,
		"adminLocale": s.adminLocale(),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	current := r.Context().Value(sessionContextKey{}).(sessionContext)
	if !validCSRF(r, current.Session.CSRFToken) {
		s.writeError(w, http.StatusForbidden, "invalid_csrf", "The CSRF token is invalid.", nil)
		return
	}
	s.sessions.Delete(current.Token)
	s.recordSecurityEvent(r, "logout", "succeeded", current.Session.Username)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	current := r.Context().Value(sessionContextKey{}).(sessionContext)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"username":    current.Session.Username,
		"csrfToken":   current.Session.CSRFToken,
		"expiresAt":   current.Session.ExpiresAt,
		"adminLocale": s.adminLocale(),
	})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	current := r.Context().Value(sessionContextKey{}).(sessionContext)
	var request changePasswordRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if len(request.NewPassword) < 12 || len(request.NewPassword) > 256 {
		s.recordSecurityEvent(r, "password-change", "rejected", current.Session.Username)
		s.writeError(w, http.StatusUnprocessableEntity, "password_invalid", "The new password must contain 12 to 256 characters.", map[string]string{"newPassword": "Use 12 to 256 characters."})
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var admin domain.AdminConfig
	if err := s.repository.ReadYAML("config/admin.yaml", &admin); err != nil {
		s.logger.Error("read administrator for password change failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "authentication_unavailable", "Authentication is temporarily unavailable.", nil)
		return
	}
	currentMatches, err := auth.VerifyPassword(request.CurrentPassword, admin.PasswordHash)
	if err != nil {
		s.logger.Error("verify current administrator password failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "authentication_unavailable", "Authentication is temporarily unavailable.", nil)
		return
	}
	if !currentMatches {
		s.recordSecurityEvent(r, "password-change", "failed", current.Session.Username)
		s.writeError(w, http.StatusUnprocessableEntity, "current_password_invalid", "The current password is incorrect.", map[string]string{"currentPassword": "The current password is incorrect."})
		return
	}
	unchanged, err := auth.VerifyPassword(request.NewPassword, admin.PasswordHash)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "authentication_unavailable", "Authentication is temporarily unavailable.", nil)
		return
	}
	if unchanged {
		s.recordSecurityEvent(r, "password-change", "rejected", current.Session.Username)
		s.writeError(w, http.StatusUnprocessableEntity, "password_unchanged", "Choose a password different from the current password.", map[string]string{"newPassword": "Choose a different password."})
		return
	}
	hash, err := auth.HashPassword(request.NewPassword)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "password_change_failed", "The password could not be changed.", nil)
		return
	}
	admin.PasswordHash = hash
	admin.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML("config/admin.yaml", admin, true); err != nil {
		s.logger.Error("write changed administrator password failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "password_change_failed", "The password could not be changed.", nil)
		return
	}
	s.sessions.Clear()
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	s.logger.Info("administrator password changed; all sessions invalidated", "username", admin.Username)
	s.recordSecurityEvent(r, "password-change", "succeeded", admin.Username)
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "succeeded", "reauthenticate": true})
}

func (s *Server) adminLocale() string {
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err == nil && site.AdminLocale != "" {
		return site.AdminLocale
	}
	return "zh-CN"
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			s.writeError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.", nil)
			return
		}
		session, ok := s.sessions.Get(cookie.Value)
		if !ok {
			s.writeError(w, http.StatusUnauthorized, "session_expired", "The session has expired.", nil)
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, sessionContext{Token: cookie.Value, Session: session})
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdminWithGate(next, false, true)
}

func (s *Server) requireExclusiveAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdminWithGate(next, true, true)
}

// requireAdminWithoutProjectionSync is for durable operations that rebuild only
// derived state themselves. They still require the normal session, CSRF, and
// shared mutation gate, but must not synchronously rebuild the same projection
// once more while their own task is queued.
func (s *Server) requireAdminWithoutProjectionSync(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAdminWithGate(next, false, false)
}

func (s *Server) requireAdminWithGate(next http.HandlerFunc, exclusive, syncProjection bool) http.HandlerFunc {
	return s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		managedMutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		if managedMutation {
			current := r.Context().Value(sessionContextKey{}).(sessionContext)
			if !validCSRF(r, current.Session.CSRFToken) {
				s.writeError(w, http.StatusForbidden, "invalid_csrf", "The CSRF token is invalid.", nil)
				return
			}
		}
		if exclusive {
			s.mutationGate.Lock()
			defer s.mutationGate.Unlock()
		} else {
			s.mutationGate.RLock()
			defer s.mutationGate.RUnlock()
		}
		if managedMutation && syncProjection {
			defer s.syncProjectionAfterManagedMutation()
		}
		next(w, r)
	})
}

func (s *Server) syncProjectionAfterManagedMutation() {
	if s.projection == nil {
		return
	}
	if _, err := s.projection.RebuildIfChanged(); err != nil {
		// The projection is derived state and the watcher will retry it. The
		// already-durable domain mutation remains the authoritative result.
		s.logger.Error("refresh projection after managed mutation failed", "error", err)
	}
}

func (s *Server) withSharedMutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mutationGate.RLock()
		defer s.mutationGate.RUnlock()
		next(w, r)
	}
}

func validCSRF(r *http.Request, expected string) bool {
	provided := r.Header.Get("X-CSRF-Token")
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
