package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/scheduled"
)

func (s *Server) handleListPages(w http.ResponseWriter, _ *http.Request) {
	pages, err := s.content.ListPages()
	if err != nil {
		s.logger.Error("list pages failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "pages_unavailable", "Cannot list pages.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"page": 1, "size": len(pages), "total": len(pages), "items": pages})
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var request createPostRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	page, err := s.content.CreatePage(content.CreatePageInput{
		ID: request.ID, Title: request.Title, Summary: request.Summary,
		SEOTitle: request.SEOTitle, SEODescription: request.SEODescription, Markdown: request.Markdown,
	})
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, page)
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.content.GetPage(r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleUpdatePageLocale(w http.ResponseWriter, r *http.Request) {
	if !s.requireEditableContentLocale(w, r) {
		return
	}
	var request updatePostLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	page, err := s.content.UpdatePageLocale(r.PathValue("id"), r.PathValue("locale"), content.UpdateLocaleInput{
		ExpectedRevision: request.Revision, Title: request.Title, Summary: request.Summary,
		SEOTitle: request.SEOTitle, SEODescription: request.SEODescription, Markdown: request.Markdown,
	})
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	if !s.invalidateScheduledContent(w, "Page", page.Meta.ID, page.Meta.Revision) {
		return
	}
	s.writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleUpdatePageSettings(w http.ResponseWriter, r *http.Request) {
	var request updatePostSettingsRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if request.Template == "" {
		request.Template = "page"
	}
	settings, err := contentSettingsInput(request)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "content_invalid", err.Error(), nil)
		return
	}
	settings.Categories = nil
	settings.Tags = nil
	settings.Pinned = nil
	s.themeGate.RLock()
	defer s.themeGate.RUnlock()
	if supported, err := s.themes.SupportsActiveTemplate("page", request.Template); err != nil {
		s.writeThemeError(w, err)
		return
	} else if !supported {
		current, currentErr := s.content.GetPage(r.PathValue("id"))
		if currentErr != nil {
			s.writeContentError(w, currentErr)
			return
		}
		if request.Template != current.Meta.Template {
			s.writeError(w, http.StatusUnprocessableEntity, "theme_template_invalid", "The active theme does not provide this page template.", map[string]string{"template": "Choose a template provided by the active theme."})
			return
		}
	}
	page, err := s.content.UpdatePageSettings(r.PathValue("id"), settings)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	if !s.invalidateScheduledContent(w, "Page", page.Meta.ID, page.Meta.Revision) {
		return
	}
	s.writeJSON(w, http.StatusOK, page)
}

func (s *Server) handlePublishPage(w http.ResponseWriter, r *http.Request) {
	var request publishPostRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	before, err := s.content.GetPage(r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	if before.Meta.PublishedAt != nil && before.Meta.PublishedAt.After(time.Now().UTC()) {
		task, scheduleErr := s.scheduler.Start(scheduled.StartInput{EntityKind: "Page", EntityID: before.Meta.ID, Revision: request.Revision, DueAt: *before.Meta.PublishedAt})
		if scheduleErr != nil {
			s.logger.Error("schedule page publish failed", "page", before.Meta.ID, "error", scheduleErr)
			if errors.Is(scheduleErr, content.ErrConflict) {
				s.writeContentError(w, scheduleErr)
				return
			}
			if errors.Is(scheduleErr, scheduled.ErrTaskRunning) {
				s.writeError(w, http.StatusConflict, "scheduled_publish_running", "A scheduled publication is already running for this page.", nil)
				return
			}
			s.writeError(w, http.StatusUnprocessableEntity, "scheduled_publish_failed", "The future publication could not be scheduled.", nil)
			return
		}
		s.writeJSON(w, http.StatusAccepted, map[string]any{
			"post":        before,
			"build":       map[string]any{"status": "scheduled", "taskId": task.ID, "dueAt": task.DueAt},
			"translation": map[string]any{"status": "deferred"},
		})
		return
	}
	page, err := s.content.PublishPage(r.PathValue("id"), request.Revision)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.clearPublicStatsCache()
	if s.scheduler != nil {
		if _, cancelErr := s.scheduler.CancelForEntity("Page", before.Meta.ID); cancelErr != nil {
			s.logger.Error("cancel superseded scheduled page publish failed after successful publish", "page", before.Meta.ID, "error", cancelErr)
		}
	}
	translationPlan := s.preparePublishTranslation("Page", page.Meta.ID)
	s.launchPublishTranslation(translationPlan)
	translationState := translationPlan.response
	if translationPlan.deferBuild {
		w.Header().Set("X-MutiBlog-Static-Build", "deferred")
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": page, "build": map[string]any{"status": "deferred"}, "translation": translationState})
		return
	}
	if translationPlan.blockBuild {
		w.Header().Set("X-MutiBlog-Static-Build", "blocked")
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": page, "build": map[string]any{"status": "blocked"}, "translation": translationState})
		return
	}
	report, buildErr := s.publisher.Build(r.Context())
	if buildErr != nil {
		s.logger.Error("static build after page publish failed", "page", page.Meta.ID, "error", buildErr)
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": page, "build": map[string]any{"status": "failed"}, "translation": translationState})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"post": page, "build": map[string]any{"status": "succeeded", "report": report}, "translation": translationState})
}
