package server

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"golang.org/x/text/language"
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,31}$`)

type setupRequest struct {
	SiteTitle    string `json:"siteTitle"`
	SourceLocale string `json:"sourceLocale"`
	AdminLocale  string `json:"adminLocale"`
	Timezone     string `json:"timezone"`
	Username     string `json:"username"`
	Password     string `json:"password"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	initialized, err := s.repository.Exists("config/initialized")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "repository_error", "Cannot read setup state.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"initialized": initialized})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	initialized, err := s.repository.Exists("config/initialized")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "repository_error", "Cannot read setup state.", nil)
		return
	}
	if initialized {
		s.writeError(w, http.StatusConflict, "already_initialized", "MutiBlog is already initialized.", nil)
		return
	}

	var request setupRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	fields := validateSetup(&request)
	if len(fields) > 0 {
		s.writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Some setup fields are invalid.", fields)
		return
	}

	sourceTag, _ := language.Parse(request.SourceLocale)
	adminTag, _ := language.Parse(request.AdminLocale)
	request.SourceLocale = sourceTag.String()
	request.AdminLocale = adminTag.String()
	now := time.Now().UTC()
	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "password_hash_failed", "Cannot secure the administrator password.", nil)
		return
	}

	site := domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  request.SourceLocale,
		AdminLocale:   request.AdminLocale,
		Timezone:      request.Timezone,
		ActiveTheme:   "earth",
		Locales: map[string]domain.LocalizedSite{
			request.SourceLocale: {Title: strings.TrimSpace(request.SiteTitle)},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  request.SourceLocale,
		Enabled: []domain.LocaleDefinition{{
			Code: request.SourceLocale, Label: request.SourceLocale, Enabled: true,
		}},
		Fallback: []string{"en", "zh-CN"},
	}
	admin := domain.AdminConfig{
		SchemaVersion: domain.SchemaVersion,
		Username:      request.Username,
		PasswordHash:  passwordHash,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	secrets := domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}}

	for _, write := range []struct {
		path   string
		value  any
		secret bool
	}{
		{"config/secrets.yaml", secrets, true},
		{"config/locales.yaml", locales, false},
		{"config/site.yaml", site, false},
		{"config/admin.yaml", admin, true},
	} {
		if err := s.repository.WriteYAML(write.path, write.value, write.secret); err != nil {
			s.logger.Error("setup write failed", "path", write.path, "error", err)
			s.writeError(w, http.StatusInternalServerError, "setup_write_failed", "Cannot save the initial configuration.", nil)
			return
		}
	}
	if err := s.repository.WriteFile("config/initialized", []byte(now.Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		s.writeError(w, http.StatusInternalServerError, "setup_write_failed", "Cannot finalize the initial configuration.", nil)
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"initialized":  true,
		"sourceLocale": request.SourceLocale,
		"adminLocale":  request.AdminLocale,
	})
}

func validateSetup(request *setupRequest) map[string]string {
	fields := make(map[string]string)
	if title := strings.TrimSpace(request.SiteTitle); title == "" || len([]rune(title)) > 80 {
		fields["siteTitle"] = "Site title must contain 1 to 80 characters."
	}
	if _, err := language.Parse(request.SourceLocale); err != nil || strings.TrimSpace(request.SourceLocale) == "" {
		fields["sourceLocale"] = "Source locale must be a valid BCP 47 language tag."
	}
	if _, err := language.Parse(request.AdminLocale); err != nil || strings.TrimSpace(request.AdminLocale) == "" {
		fields["adminLocale"] = "Admin locale must be a valid BCP 47 language tag."
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil || strings.TrimSpace(request.Timezone) == "" {
		fields["timezone"] = "Timezone must be a valid IANA timezone."
	}
	if !usernamePattern.MatchString(request.Username) {
		fields["username"] = "Username must start with a letter and contain 3 to 32 lowercase letters, numbers, or hyphens."
	}
	if len(request.Password) < 12 || len(request.Password) > 256 {
		fields["password"] = "Password must contain 12 to 256 characters."
	}
	return fields
}
