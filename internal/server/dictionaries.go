package server

import (
	"net/http"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
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
	s.writeError(w, http.StatusUnprocessableEntity, "dictionary_managed", "Framework dictionaries are managed by site-wide localization and cannot be edited directly.", nil)
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
