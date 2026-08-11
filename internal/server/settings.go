package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"golang.org/x/text/language"
)

type updateSiteSettingsRequest struct {
	BaseURL     string `json:"baseUrl"`
	Timezone    string `json:"timezone"`
	AdminLocale string `json:"adminLocale"`
	PrimaryMenu string `json:"primaryMenu"`
	IDStrategy  string `json:"idStrategy"`
}
type updateSiteLocaleRequest struct {
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Description string `json:"description"`
}
type updateCommentSettingsRequest struct {
	Moderation string `json:"moderation"`
	PageSize   int    `json:"pageSize"`
	MaxLength  int    `json:"maxLength"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	var site domain.SiteConfig
	var comments domain.CommentsConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeError(w, 500, "settings_unavailable", "Settings are unavailable.", nil)
		return
	}
	if site.IDStrategy == "" {
		site.IDStrategy = content.IDStrategyUUID
	}
	if err := s.repository.ReadYAML("config/comments.yaml", &comments); err != nil {
		comments = domain.CommentsConfig{SchemaVersion: 1, Moderation: "pending", PageSize: 20, MaxLength: 2000}
	}
	s.writeJSON(w, 200, map[string]any{"site": site, "comments": comments})
}
func (s *Server) handleUpdateSiteSettings(w http.ResponseWriter, r *http.Request) {
	var request updateSiteSettingsRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	baseURL, err := normalizeBaseURL(request.BaseURL)
	if err != nil {
		s.writeError(w, 422, "site_settings_invalid", "The public base URL is invalid.", nil)
		return
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		s.writeError(w, 422, "site_settings_invalid", "The timezone is invalid.", nil)
		return
	}
	adminTag, err := language.Parse(request.AdminLocale)
	if err != nil || (adminTag.String() != "en" && adminTag.String() != "zh-CN") {
		s.writeError(w, 422, "site_settings_invalid", "The console locale is not installed.", nil)
		return
	}
	if request.PrimaryMenu != "" {
		if _, err := s.menus.Get(request.PrimaryMenu); err != nil {
			s.writeError(w, http.StatusUnprocessableEntity, "site_settings_invalid", "The primary menu does not exist.", map[string]string{"primaryMenu": "Choose an existing menu."})
			return
		}
	}
	idStrategy, valid := content.NormalizeIDStrategy(request.IDStrategy)
	if !valid {
		s.writeError(w, http.StatusUnprocessableEntity, "site_settings_invalid", "The default ID strategy is invalid.", map[string]string{"idStrategy": "Choose compact UUID or millisecond timestamp."})
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeError(w, 500, "settings_unavailable", "Settings are unavailable.", nil)
		return
	}
	site.BaseURL = baseURL
	site.Timezone = request.Timezone
	site.AdminLocale = adminTag.String()
	site.PrimaryMenu = request.PrimaryMenu
	site.IDStrategy = idStrategy
	site.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML("config/site.yaml", site, false); err != nil {
		s.writeError(w, 500, "settings_save_failed", "Cannot save settings.", nil)
		return
	}
	s.rebuildAfterSettings(w, r, site)
}
func (s *Server) handleUpdateSiteLocale(w http.ResponseWriter, r *http.Request) {
	var request updateSiteLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	tag, err := language.Parse(r.PathValue("locale"))
	if err != nil {
		s.writeError(w, 422, "locale_disabled", "The locale is not enabled.", nil)
		return
	}
	locale := tag.String()
	if strings.TrimSpace(request.Title) == "" || len([]rune(request.Title)) > 80 {
		s.writeError(w, 422, "site_locale_invalid", "The site title is required.", nil)
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var locales domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		s.writeError(w, 500, "settings_unavailable", "Settings are unavailable.", nil)
		return
	}
	enabled := false
	for _, item := range locales.Enabled {
		if item.Enabled && item.Code == locale {
			enabled = true
			break
		}
	}
	if !enabled {
		s.writeError(w, 422, "locale_disabled", "The locale is not enabled.", nil)
		return
	}
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeError(w, 500, "settings_unavailable", "Settings are unavailable.", nil)
		return
	}
	site.Locales[locale] = domain.LocalizedSite{Title: strings.TrimSpace(request.Title), Subtitle: strings.TrimSpace(request.Subtitle), Description: strings.TrimSpace(request.Description)}
	site.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML("config/site.yaml", site, false); err != nil {
		s.writeError(w, 500, "settings_save_failed", "Cannot save settings.", nil)
		return
	}
	s.rebuildAfterSettings(w, r, site)
}
func (s *Server) handleUpdateCommentSettings(w http.ResponseWriter, r *http.Request) {
	var request updateCommentSettingsRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if (request.Moderation != "pending" && request.Moderation != "none") || request.PageSize < 1 || request.PageSize > 100 || request.MaxLength < 100 || request.MaxLength > 10000 {
		s.writeError(w, 422, "comment_settings_invalid", "The comment settings are invalid.", nil)
		return
	}
	settings := domain.CommentsConfig{SchemaVersion: 1, Moderation: request.Moderation, PageSize: request.PageSize, MaxLength: request.MaxLength}
	if err := s.repository.WriteYAML("config/comments.yaml", settings, false); err != nil {
		s.writeError(w, 500, "settings_save_failed", "Cannot save settings.", nil)
		return
	}
	s.writeJSON(w, 200, settings)
}
func (s *Server) rebuildAfterSettings(w http.ResponseWriter, r *http.Request, site domain.SiteConfig) {
	report, err := s.publisher.Build(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusAccepted, map[string]any{"site": site, "build": map[string]any{"status": "failed"}})
		return
	}
	s.writeJSON(w, 200, map[string]any{"site": site, "build": map[string]any{"status": "succeeded", "report": report}})
}
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", http.ErrNotSupported
	}
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}
