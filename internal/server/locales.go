package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	linkservice "github.com/FengYuchen1314/mutiblog/internal/links"
	menuservice "github.com/FengYuchen1314/mutiblog/internal/menus"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
	"golang.org/x/text/language"
)

type updateLocalesRequest struct {
	SourceLocale string                    `json:"sourceLocale"`
	Enabled      []domain.LocaleDefinition `json:"enabled"`
}

func (s *Server) handleLocales(w http.ResponseWriter, _ *http.Request) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		s.logger.Error("read locales failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "Cannot read locale configuration.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateLocales(w http.ResponseWriter, r *http.Request) {
	var request updateLocalesRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var current domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &current); err != nil {
		s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "Cannot read locale settings.", nil)
		return
	}
	requestedSource := strings.TrimSpace(request.SourceLocale)
	if requestedSource == "" {
		requestedSource = current.SourceLocale
	}
	sourceTag, err := language.Parse(requestedSource)
	if err != nil || requestedSource == "" {
		s.writeError(w, http.StatusUnprocessableEntity, "source_locale_invalid", "The source locale must be a valid BCP 47 language tag.", nil)
		return
	}
	requestedSource = sourceTag.String()
	definitions := make([]domain.LocaleDefinition, 0, len(request.Enabled))
	seen := make(map[string]bool, len(request.Enabled))
	sourceEnabled := false
	for index, definition := range request.Enabled {
		tag, err := language.Parse(strings.TrimSpace(definition.Code))
		if err != nil || strings.TrimSpace(definition.Code) == "" {
			s.writeError(w, http.StatusUnprocessableEntity, "locale_invalid", "A locale code is not valid BCP 47.", map[string]string{"enabled": "Invalid locale at index " + strconv.Itoa(index) + "."})
			return
		}
		code := tag.String()
		label := strings.TrimSpace(definition.Label)
		if label == "" || len([]rune(label)) > 80 {
			s.writeError(w, http.StatusUnprocessableEntity, "locale_label_invalid", "Locale labels must contain 1 to 80 characters.", nil)
			return
		}
		if seen[code] {
			s.writeError(w, http.StatusConflict, "locale_duplicate", "Locale codes must be unique.", nil)
			return
		}
		seen[code] = true
		definition.Code = code
		definition.Label = label
		if code == requestedSource {
			definition.Enabled = true
			sourceEnabled = true
		}
		definitions = append(definitions, definition)
	}
	if !sourceEnabled {
		s.writeError(w, http.StatusUnprocessableEntity, "source_locale_required", "The selected source locale must be present and enabled.", nil)
		return
	}
	referencedSources, err := s.permanentSourceLocales()
	if err != nil {
		s.logger.Error("read permanent source locales failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "Cannot validate locale usage.", nil)
		return
	}
	for locale := range referencedSources {
		if !seen[locale] || !definitionsEnabled(definitions, locale) {
			s.writeError(w, http.StatusUnprocessableEntity, "locale_in_use", "A locale used as the original source of existing content cannot be disabled.", map[string]string{"enabled": locale})
			return
		}
	}
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeError(w, http.StatusInternalServerError, "site_unavailable", "Cannot read site settings.", nil)
		return
	}
	previous := current
	current.SourceLocale = requestedSource
	current.Enabled = definitions
	current.Fallback = []string{"en", "zh-CN"}
	if err := s.repository.WriteYAML("config/locales.yaml", current, false); err != nil {
		s.writeError(w, http.StatusInternalServerError, "locales_write_failed", "Cannot save locale settings.", nil)
		return
	}
	site.SourceLocale = requestedSource
	site.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML("config/site.yaml", site, false); err != nil {
		if rollbackErr := s.repository.WriteYAML("config/locales.yaml", previous, false); rollbackErr != nil {
			s.logger.Error("rollback locale settings failed", "error", rollbackErr)
		}
		s.writeError(w, http.StatusInternalServerError, "site_write_failed", "Cannot save the new source locale.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, current)
}

func (s *Server) permanentSourceLocales() (map[string]bool, error) {
	result := make(map[string]bool)
	posts, err := content.NewService(s.repository).ListPosts()
	if err != nil {
		return nil, err
	}
	pages, err := content.NewService(s.repository).ListPages()
	if err != nil {
		return nil, err
	}
	for _, item := range append(posts, pages...) {
		result[item.Meta.SourceLocale] = true
	}
	taxonomyService := taxonomy.NewService(s.repository)
	for _, kind := range []string{"Category", "Tag"} {
		items, err := taxonomyService.List(kind)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			result[item.SourceLocale] = true
		}
	}
	linksService := linkservice.NewService(s.repository)
	groups, err := linksService.ListGroups()
	if err != nil {
		return nil, err
	}
	links, err := linksService.ListLinks()
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		result[group.SourceLocale] = true
	}
	for _, link := range links {
		result[link.SourceLocale] = true
	}
	menus, err := menuservice.NewService(s.repository).List()
	if err != nil {
		return nil, err
	}
	for _, menu := range menus {
		result[menu.SourceLocale] = true
	}
	return result, nil
}

func definitionsEnabled(definitions []domain.LocaleDefinition, locale string) bool {
	for _, definition := range definitions {
		if definition.Code == locale {
			return definition.Enabled
		}
	}
	return false
}
