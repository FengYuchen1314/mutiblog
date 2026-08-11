package server

import (
	"net/http"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"golang.org/x/text/language"
)

type frameworkDictionaryView struct {
	Locale     string            `json:"locale"`
	Translated int               `json:"translated"`
	Total      int               `json:"total"`
	Missing    []string          `json:"missing"`
	Values     map[string]string `json:"values"`
}

func (s *Server) handleFrameworkDictionaries(w http.ResponseWriter, _ *http.Request) {
	views, err := s.frameworkDictionaryViews()
	if err != nil {
		s.logger.Error("read framework dictionaries failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "dictionaries_unavailable", "The framework dictionaries are unavailable.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (s *Server) handleUpdateFrameworkDictionary(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Values map[string]string `json:"values"`
	}
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	tag, err := language.Parse(r.PathValue("locale"))
	if err != nil || !s.frameworkLocaleEnabled(tag.String()) {
		s.writeError(w, http.StatusUnprocessableEntity, "locale_disabled", "The locale is not enabled.", nil)
		return
	}
	allowed := make(map[string]bool)
	for _, key := range dictionary.RequiredKeys() {
		allowed[key] = true
	}
	for key, value := range request.Values {
		if !allowed[key] || len([]rune(value)) > 500 {
			s.writeError(w, http.StatusUnprocessableEntity, "dictionary_invalid", "The framework dictionary contains an unsupported key or value.", nil)
			return
		}
	}
	if err := dictionary.Write(s.repository, tag.String(), request.Values); err != nil {
		s.logger.Error("write framework dictionary failed", "locale", tag.String(), "error", err)
		s.writeError(w, http.StatusInternalServerError, "dictionary_save_failed", "The framework dictionary could not be saved.", nil)
		return
	}
	views, err := s.frameworkDictionaryViews()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "dictionaries_unavailable", "The framework dictionaries are unavailable.", nil)
		return
	}
	for _, view := range views {
		if view.Locale == tag.String() {
			s.writePublishedResource(w, r, http.StatusOK, view, "Dictionary", tag.String())
			return
		}
	}
	s.writeError(w, http.StatusInternalServerError, "dictionaries_unavailable", "The framework dictionary is unavailable.", nil)
}

func (s *Server) frameworkLocaleEnabled(locale string) bool {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return false
	}
	for _, item := range config.Enabled {
		if item.Enabled && item.Code == locale {
			return true
		}
	}
	return false
}

func (s *Server) frameworkDictionaryViews() ([]frameworkDictionaryView, error) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return nil, err
	}
	dictionaries, err := dictionary.Read(s.repository)
	if err != nil {
		return nil, err
	}
	keys := dictionary.RequiredKeys()
	views := make([]frameworkDictionaryView, 0, len(config.Enabled))
	for _, locale := range config.Enabled {
		if !locale.Enabled {
			continue
		}
		values := dictionaries[locale.Code]
		if values == nil {
			values = map[string]string{}
		}
		missing := make([]string, 0)
		visible := make(map[string]string, len(keys))
		for _, key := range keys {
			visible[key] = values[key]
			if strings.TrimSpace(values[key]) == "" {
				missing = append(missing, key)
			}
		}
		views = append(views, frameworkDictionaryView{Locale: locale.Code, Translated: len(keys) - len(missing), Total: len(keys), Missing: missing, Values: visible})
	}
	return views, nil
}
