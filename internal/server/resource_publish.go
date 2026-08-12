package server

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
)

// writePublishedResource keeps admin resource responses backwards compatible
// while ensuring resources without a draft lifecycle are reflected in the
// static public release immediately. A failed build never replaces the current
// healthy release; 202 and the response header make that degraded result
// observable without changing the resource JSON shape.
func (s *Server) writePublishedResource(w http.ResponseWriter, r *http.Request, successStatus int, resource any, kind, id string, requiresLocalization bool) {
	if requiresLocalization && s.deferResourceBuildForLocalization(w, resource, kind, id) {
		return
	}
	if _, err := s.publisher.Build(r.Context()); err != nil {
		if s.logger != nil {
			s.logger.Error("static build after public resource update failed", "kind", kind, "id", id, "error", err)
		}
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, resource)
		return
	}
	w.Header().Set("X-MutiBlog-Static-Build", "succeeded")
	s.writeJSON(w, successStatus, resource)
}

func (s *Server) writePublishedDeletion(w http.ResponseWriter, r *http.Request, kind, id string) {
	if _, err := s.publisher.Build(r.Context()); err != nil {
		if s.logger != nil {
			s.logger.Error("static build after public resource deletion failed", "kind", kind, "id", id, "error", err)
		}
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, map[string]any{"deleted": true})
		return
	}
	w.Header().Set("X-MutiBlog-Static-Build", "succeeded")
	w.WriteHeader(http.StatusNoContent)
}

func sourceLocaleWasUpdated(sourceLocale, rawLocale string) bool {
	return strings.EqualFold(strings.TrimSpace(sourceLocale), strings.TrimSpace(rawLocale))
}

// deferResourceBuildForLocalization gives source-only public resources the
// same durable, one-build lifecycle as first-published content. A ready target
// locale is deliberately kept public while the refresh runs; the worker
// therefore must finish every target before it creates its replacement static
// release. With only the fixed source locale this is a normal synchronous
// build, preserving the previous response behavior.
func (s *Server) deferResourceBuildForLocalization(w http.ResponseWriter, resource any, kind, id string) bool {
	targets, err := s.readyResourceLocalizationTargets()
	if err == nil && len(targets) == 0 {
		return false
	}
	if err == nil {
		manager := s.localeTaskManager
		if manager == nil && s.localeTasks != nil {
			manager = s.localeTasks
		}
		if manager != nil {
			task, startErr := manager.Start(localization.LocaleProvisionStartInput{
				Locales:                  targets,
				PreserveLocaleVisibility: true,
			})
			if startErr == nil {
				w.Header().Set("X-MutiBlog-Static-Build", "deferred")
				w.Header().Set("X-MutiBlog-Localization-Task", task.ID)
				s.writeJSON(w, http.StatusAccepted, resource)
				return true
			}
			err = startErr
		} else {
			err = errors.New("locale task service is unavailable")
		}
	}
	if s.logger != nil {
		s.logger.Error("queue localization after public resource update failed", "kind", kind, "id", id, "error", err)
	}
	// The mutation has already been committed. Do not send it through the
	// strict renderer with missing target fields: preserve the current release
	// and make the deferred recovery state explicit to the console instead.
	w.Header().Set("X-MutiBlog-Static-Build", "deferred")
	w.Header().Set("X-MutiBlog-Localization", "unavailable")
	s.writeJSON(w, http.StatusAccepted, resource)
	return true
}

// readyResourceLocalizationTargets returns only locales that are currently
// public. Empty lifecycle status is retained as the legacy ready form; a
// provisioning or failed locale has its own task and must not be bundled into
// a resource refresh.
func (s *Server) readyResourceLocalizationTargets() ([]string, error) {
	if s == nil || s.repository == nil {
		return nil, nil
	}
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(config.Enabled))
	for _, definition := range config.Enabled {
		status := strings.TrimSpace(definition.Status)
		if !definition.Enabled || definition.Code == localeconfig.FixedSourceLocale || definition.Code == config.SourceLocale {
			continue
		}
		if status == "" || status == domain.LocaleStatusReady {
			targets = append(targets, definition.Code)
		}
	}
	sort.Strings(targets)
	return targets, nil
}
