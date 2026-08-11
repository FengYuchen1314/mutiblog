package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type createPostRequest struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Markdown       string `json:"markdown"`
}

type updatePostLocaleRequest struct {
	Revision       int    `json:"revision"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Markdown       string `json:"markdown"`
}

type publishPostRequest struct {
	Revision int `json:"revision"`
}

type updatePostSettingsRequest struct {
	Revision      int      `json:"revision"`
	Categories    []string `json:"categories"`
	Tags          []string `json:"tags"`
	Cover         string   `json:"cover"`
	CommentPolicy string   `json:"commentPolicy"`
	Template      string   `json:"template"`
}

func (s *Server) handleListPosts(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.content.ListPosts()
	if err != nil {
		s.logger.Error("list posts failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "posts_unavailable", "Cannot list posts.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"page": 1, "size": len(posts), "total": len(posts), "items": posts})
}

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	var request createPostRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	post, err := s.content.CreatePost(content.CreatePostInput{
		ID: request.ID, Title: request.Title, Summary: request.Summary,
		SEOTitle: request.SEOTitle, SEODescription: request.SEODescription, Markdown: request.Markdown,
	})
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, post)
}

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	post, err := s.content.GetPost(r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, post)
}

func (s *Server) handleUpdatePostLocale(w http.ResponseWriter, r *http.Request) {
	var request updatePostLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	post, err := s.content.UpdateLocale(r.PathValue("id"), r.PathValue("locale"), content.UpdateLocaleInput{
		ExpectedRevision: request.Revision,
		Title:            request.Title, Summary: request.Summary, SEOTitle: request.SEOTitle,
		SEODescription: request.SEODescription, Markdown: request.Markdown,
	})
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, post)
}

func (s *Server) handleUpdatePostSettings(w http.ResponseWriter, r *http.Request) {
	var request updatePostSettingsRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if request.Template == "" {
		request.Template = "post"
	}
	s.themeGate.RLock()
	defer s.themeGate.RUnlock()
	if supported, err := s.themes.SupportsActiveTemplate("post", request.Template); err != nil {
		s.writeThemeError(w, err)
		return
	} else if !supported {
		current, currentErr := s.content.GetPost(r.PathValue("id"))
		if currentErr != nil {
			s.writeContentError(w, currentErr)
			return
		}
		if request.Template != current.Meta.Template {
			s.writeError(w, http.StatusUnprocessableEntity, "theme_template_invalid", "The active theme does not provide this post template.", map[string]string{"template": "Choose a template provided by the active theme."})
			return
		}
	}
	post, err := s.content.UpdatePostSettings(r.PathValue("id"), content.UpdatePostSettingsInput{
		ExpectedRevision: request.Revision, Categories: request.Categories, Tags: request.Tags,
		Cover: request.Cover, CommentPolicy: request.CommentPolicy, Template: request.Template,
	})
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, post)
}

func (s *Server) handlePublishPost(w http.ResponseWriter, r *http.Request) {
	var request publishPostRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	before, err := s.content.GetPost(r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	firstPublish := before.Meta.PublishedAt == nil
	post, err := s.content.PublishPost(r.PathValue("id"), request.Revision)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	report, buildErr := s.publisher.Build(r.Context())
	translationState := map[string]any{"status": "not-needed"}
	if firstPublish {
		task, translationErr := s.translator.Start(translation.StartInput{PostID: post.Meta.ID, SkipManual: true})
		switch {
		case translationErr == nil:
			translationState = map[string]any{"status": "queued", "taskId": task.ID}
		case errors.Is(translationErr, translation.ErrNoTargets):
			translationState = map[string]any{"status": "not-needed"}
		case errors.Is(translationErr, ai.ErrProviderNotFound), errors.Is(translationErr, ai.ErrKeyMissing), errors.Is(translationErr, ai.ErrInvalidProvider):
			translationState = map[string]any{"status": "not-configured"}
		default:
			s.logger.Error("automatic first-publish translation could not start", "post", post.Meta.ID, "error", translationErr)
			translationState = map[string]any{"status": "failed"}
		}
	}
	if buildErr != nil {
		s.logger.Error("static build after publish failed", "post", post.Meta.ID, "error", buildErr)
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": post, "build": map[string]any{"status": "failed"}, "translation": translationState})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"post": post, "build": map[string]any{"status": "succeeded", "report": report}, "translation": translationState})
}

func (s *Server) handleBuildSite(w http.ResponseWriter, r *http.Request) {
	report, err := s.publisher.Build(r.Context())
	if err != nil {
		s.logger.Error("manual static build failed", "error", err)
		s.writeError(w, http.StatusServiceUnavailable, "static_build_failed", "The previous public release is still active.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "succeeded", "report": report})
}

func (s *Server) writeContentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "post_not_found", "The post does not exist.", nil)
	case errors.Is(err, content.ErrConflict):
		s.writeError(w, http.StatusConflict, "revision_conflict", "The post changed after it was loaded.", nil)
	case errors.Is(err, content.ErrAlreadyExists):
		s.writeError(w, http.StatusConflict, "post_id_exists", "A post with this ID already exists.", map[string]string{"id": "Choose another custom ID."})
	case errors.Is(err, content.ErrInvalidID):
		s.writeError(w, http.StatusUnprocessableEntity, "invalid_post_id", "The custom ID must contain lowercase letters and hyphens only.", map[string]string{"id": "Use lowercase letters separated by single hyphens."})
	case errors.Is(err, content.ErrLocaleDisabled):
		s.writeError(w, http.StatusUnprocessableEntity, "locale_disabled", "The locale is not enabled for this site.", nil)
	case errors.Is(err, content.ErrInvalidStatus):
		s.writeError(w, http.StatusConflict, "content_status_invalid", "The content cannot perform this action in its current status.", nil)
	default:
		s.logger.Error("content operation failed", "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "content_invalid", err.Error(), nil)
	}
}
