package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/themes"
)

type themePreviewPublisher interface {
	Preview(context.Context, string, string) (publisher.PreviewRecord, error)
	PreviewRoot(string) (publisher.PreviewRecord, string, error)
}

func (s *Server) handleCreateThemePreview(w http.ResponseWriter, r *http.Request) {
	s.themeGate.RLock()
	defer s.themeGate.RUnlock()

	id := r.PathValue("id")
	view, err := s.themeView(id)
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	if view.Status != "ready" {
		s.writeThemeError(w, themes.ErrIncompatible)
		return
	}
	origin, err := s.previewOrigin(r)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "base_url_invalid", "Configure a valid site base URL before creating a theme preview.", nil)
		return
	}
	previewer, ok := s.publisher.(themePreviewPublisher)
	if !ok {
		s.writeError(w, http.StatusServiceUnavailable, "theme_preview_unavailable", "Theme previews are unavailable.", nil)
		return
	}
	record, err := previewer.Preview(r.Context(), id, origin)
	if err != nil {
		s.logger.Warn("theme preview build failed", "theme", id, "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "theme_render_failed", "The theme preview could not be rendered; the public release is unchanged.", nil)
		return
	}
	record.URL = origin + "/__mutiblog-preview/open/" + record.ID
	s.recordSecurityEvent(r, "theme-preview", "succeeded", adminActor(r))
	s.writeJSON(w, http.StatusCreated, record)
}

func (s *Server) handleOpenThemePreview(w http.ResponseWriter, r *http.Request) {
	if !s.isThemePreviewHost(r) {
		http.NotFound(w, r)
		return
	}
	previewer, ok := s.publisher.(themePreviewPublisher)
	if !ok {
		http.NotFound(w, r)
		return
	}
	record, _, err := previewer.PreviewRoot(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	origin, _ := s.previewOrigin(r)
	parsed, _ := url.Parse(origin)
	maxAge := int(time.Until(record.ExpiresAt).Seconds())
	if maxAge < 1 {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: previewCookieName, Value: record.ID, Path: "/", Expires: record.ExpiresAt, MaxAge: maxAge, HttpOnly: true, Secure: parsed.Scheme == "https", SameSite: http.SameSiteLaxMode})
	var locales domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &locales); err != nil || strings.TrimSpace(locales.SourceLocale) == "" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/"+url.PathEscape(locales.SourceLocale)+"/", http.StatusFound)
}

func (s *Server) themeView(id string) (themes.View, error) {
	items, err := s.themes.List()
	if err != nil {
		return themes.View{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return themes.View{}, themes.ErrNotFound
}
