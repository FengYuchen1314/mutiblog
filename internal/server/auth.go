package server

import (
	"context"
	"crypto/subtle"
	"net/http"
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
		time.Sleep(250 * time.Millisecond)
		s.writeError(w, http.StatusUnauthorized, "invalid_credentials", "The username or password is incorrect.", nil)
		return
	}
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
	s.writeJSON(w, http.StatusOK, map[string]any{
		"username":  admin.Username,
		"csrfToken": session.CSRFToken,
		"expiresAt": session.ExpiresAt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	current := r.Context().Value(sessionContextKey{}).(sessionContext)
	if !validCSRF(r, current.Session.CSRFToken) {
		s.writeError(w, http.StatusForbidden, "invalid_csrf", "The CSRF token is invalid.", nil)
		return
	}
	s.sessions.Delete(current.Token)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	current := r.Context().Value(sessionContextKey{}).(sessionContext)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"username":  current.Session.Username,
		"csrfToken": current.Session.CSRFToken,
		"expiresAt": current.Session.ExpiresAt,
	})
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

func validCSRF(r *http.Request, expected string) bool {
	provided := r.Header.Get("X-CSRF-Token")
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
