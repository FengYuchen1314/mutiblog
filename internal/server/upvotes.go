package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/upvotes"
)

const upvoteVisitorCookieName = "mutiblog_upvote_visitor"

type createUpvoteRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s *Server) handlePublicUpvotes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	token, err := s.upvoteVisitorToken(w, r)
	if err != nil {
		s.writeUpvoteError(w, err)
		return
	}
	result, err := s.upvotes.Get(r.URL.Query().Get("kind"), r.URL.Query().Get("id"), token)
	if err != nil {
		s.writeUpvoteError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCreatePublicUpvote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.isThemePreviewHost(r) {
		s.writeError(w, http.StatusForbidden, "preview_read_only", "Theme previews are read-only.", nil)
		return
	}
	var request createUpvoteRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	token, err := s.upvoteVisitorToken(w, r)
	if err != nil {
		s.writeUpvoteError(w, err)
		return
	}
	result, err := s.upvotes.Add(request.Kind, request.ID, token, clientIP(r))
	if err != nil {
		s.writeUpvoteError(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	s.writeJSON(w, status, result)
}

func (s *Server) upvoteVisitorToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(upvoteVisitorCookieName); err == nil {
		token := strings.ToLower(strings.TrimSpace(cookie.Value))
		if upvotes.ValidVisitorToken(token) {
			return token, nil
		}
	}
	token, err := upvotes.NewVisitorToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     upvoteVisitorCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour) / time.Second),
		HttpOnly: true,
		Secure:   secureUpvoteCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

func secureUpvoteCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !trustedProxyIP(remoteIP(r.RemoteAddr)) {
		return false
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
	return len(forwarded) > 0 && strings.EqualFold(strings.TrimSpace(forwarded[0]), "https")
}

func (s *Server) writeUpvoteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, upvotes.ErrInvalid):
		s.writeError(w, http.StatusUnprocessableEntity, "upvote_invalid", "The upvote subject or visitor identity is invalid.", nil)
	case errors.Is(err, upvotes.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "upvote_not_found", "The published content does not exist.", nil)
	case errors.Is(err, upvotes.ErrRateLimit):
		w.Header().Set("Retry-After", "600")
		s.writeError(w, http.StatusTooManyRequests, "upvote_rate_limited", "Too many upvotes were submitted. Please try again later.", nil)
	default:
		s.logger.Error("upvote operation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "upvotes_unavailable", "The upvote service is unavailable.", nil)
	}
}
