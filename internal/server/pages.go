package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
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
	page, err := s.content.UpdatePageSettings(r.PathValue("id"), content.UpdatePostSettingsInput{
		ExpectedRevision: request.Revision, Cover: request.Cover,
		CommentPolicy: request.CommentPolicy, Template: request.Template,
	})
	if err != nil {
		s.writeContentError(w, err)
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
	firstPublish := before.Meta.PublishedAt == nil
	page, err := s.content.PublishPage(r.PathValue("id"), request.Revision)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	report, buildErr := s.publisher.Build(r.Context())
	translationState := map[string]any{"status": "not-needed"}
	if firstPublish {
		task, translationErr := s.translator.Start(translation.StartInput{EntityKind: "Page", PostID: page.Meta.ID, SkipManual: true})
		switch {
		case translationErr == nil:
			translationState = map[string]any{"status": "queued", "taskId": task.ID}
		case errors.Is(translationErr, translation.ErrNoTargets):
			translationState = map[string]any{"status": "not-needed"}
		case errors.Is(translationErr, ai.ErrProviderNotFound), errors.Is(translationErr, ai.ErrKeyMissing), errors.Is(translationErr, ai.ErrInvalidProvider):
			translationState = map[string]any{"status": "not-configured"}
		default:
			s.logger.Error("automatic page translation could not start", "page", page.Meta.ID, "error", translationErr)
			translationState = map[string]any{"status": "failed"}
		}
	}
	if buildErr != nil {
		s.logger.Error("static build after page publish failed", "page", page.Meta.ID, "error", buildErr)
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": page, "build": map[string]any{"status": "failed"}, "translation": translationState})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"post": page, "build": map[string]any{"status": "succeeded", "report": report}, "translation": translationState})
}
