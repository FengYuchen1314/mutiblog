package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type startTranslationRequest struct {
	Locales         []string `json:"locales"`
	OverwriteManual bool     `json:"overwriteManual"`
}

func (s *Server) handleStartTranslation(w http.ResponseWriter, r *http.Request) {
	s.handleStartEntityTranslation(w, r, "Post")
}

func (s *Server) handleStartPageTranslation(w http.ResponseWriter, r *http.Request) {
	s.handleStartEntityTranslation(w, r, "Page")
}

func (s *Server) handleStartEntityTranslation(w http.ResponseWriter, r *http.Request, entityKind string) {
	var request startTranslationRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	task, err := s.translator.Start(translation.StartInput{EntityKind: entityKind, PostID: r.PathValue("id"), Locales: request.Locales, OverwriteManual: request.OverwriteManual})
	if err != nil {
		if errors.Is(err, translation.ErrManualConfirmation) {
			locales := make([]string, 0, len(task.Targets))
			for _, target := range task.Targets {
				locales = append(locales, target.Locale)
			}
			s.writeJSON(w, http.StatusConflict, map[string]any{"code": "manual_translation_confirmation_required", "message": "Manual translations are included and require explicit overwrite confirmation.", "locales": locales})
			return
		}
		s.writeTranslationError(w, err)
		return
	}
	s.writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleListTranslationTasks(w http.ResponseWriter, _ *http.Request) {
	tasks, err := s.translator.List()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "translation_tasks_unavailable", "Cannot list translation tasks.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": tasks})
}

func (s *Server) writeTranslationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "post_not_found", "The post does not exist.", nil)
	case errors.Is(err, content.ErrLocaleDisabled):
		s.writeError(w, http.StatusUnprocessableEntity, "locale_disabled", "A requested target locale is not enabled.", nil)
	case errors.Is(err, translation.ErrNoTargets):
		s.writeError(w, http.StatusUnprocessableEntity, "translation_targets_empty", "No enabled target locales require translation.", nil)
	case errors.Is(err, ai.ErrProviderNotFound), errors.Is(err, ai.ErrKeyMissing), errors.Is(err, ai.ErrInvalidProvider):
		s.writeError(w, http.StatusPreconditionFailed, "provider_required", "Configure an enabled default translation provider first.", nil)
	default:
		s.logger.Error("start translation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "translation_start_failed", "Cannot start the translation task.", nil)
	}
}
