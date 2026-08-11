package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/scheduled"
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
	Pinned        *bool    `json:"pinned,omitempty"`
	Visibility    *string  `json:"visibility,omitempty"`
	PublishedAt   *string  `json:"publishedAt,omitempty"`
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
	if !s.invalidateScheduledContent(w, "Post", post.Meta.ID, post.Meta.Revision) {
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
	settings, err := contentSettingsInput(request)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "content_invalid", err.Error(), nil)
		return
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
	post, err := s.content.UpdatePostSettings(r.PathValue("id"), settings)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	if !s.invalidateScheduledContent(w, "Post", post.Meta.ID, post.Meta.Revision) {
		return
	}
	s.writeJSON(w, http.StatusOK, post)
}

func contentSettingsInput(request updatePostSettingsRequest) (content.UpdatePostSettingsInput, error) {
	input := content.UpdatePostSettingsInput{
		ExpectedRevision: request.Revision,
		Categories:       request.Categories,
		Tags:             request.Tags,
		Cover:            request.Cover,
		Pinned:           request.Pinned,
		CommentPolicy:    request.CommentPolicy,
		Template:         request.Template,
	}
	if request.Visibility != nil {
		visibility := domain.ContentVisibility(strings.TrimSpace(*request.Visibility))
		input.Visibility = &visibility
	}
	if request.PublishedAt != nil {
		input.PublishTimeSet = true
		value := strings.TrimSpace(*request.PublishedAt)
		if value != "" {
			publishedAt, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return content.UpdatePostSettingsInput{}, errors.New("publish time must be an RFC 3339 timestamp")
			}
			publishedAt = publishedAt.UTC()
			input.PublishedAt = &publishedAt
		}
	}
	return input, nil
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
	firstPublish := before.Meta.ReleaseRevision == 0
	if before.Meta.PublishedAt != nil && before.Meta.PublishedAt.After(time.Now().UTC()) {
		task, scheduleErr := s.scheduler.Start(scheduled.StartInput{EntityKind: "Post", EntityID: before.Meta.ID, Revision: request.Revision, DueAt: *before.Meta.PublishedAt})
		if scheduleErr != nil {
			s.logger.Error("schedule post publish failed", "post", before.Meta.ID, "error", scheduleErr)
			if errors.Is(scheduleErr, content.ErrConflict) {
				s.writeContentError(w, scheduleErr)
				return
			}
			if errors.Is(scheduleErr, scheduled.ErrTaskRunning) {
				s.writeError(w, http.StatusConflict, "scheduled_publish_running", "A scheduled publication is already running for this post.", nil)
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
	post, err := s.content.PublishPost(r.PathValue("id"), request.Revision)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.clearPublicStatsCache()
	if s.scheduler != nil {
		if _, cancelErr := s.scheduler.CancelForEntity("Post", before.Meta.ID); cancelErr != nil {
			s.logger.Error("cancel superseded scheduled post publish failed after successful publish", "post", before.Meta.ID, "error", cancelErr)
		}
	}
	translationPlan := s.prepareFirstPublishTranslation(firstPublish, "Post", post.Meta.ID)
	report, buildErr := s.publisher.Build(r.Context())
	s.launchFirstPublishTranslation(translationPlan)
	translationState := translationPlan.response
	if buildErr != nil {
		s.logger.Error("static build after publish failed", "post", post.Meta.ID, "error", buildErr)
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": post, "build": map[string]any{"status": "failed"}, "translation": translationState})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"post": post, "build": map[string]any{"status": "succeeded", "report": report}, "translation": translationState})
}

func (s *Server) invalidateScheduledContent(w http.ResponseWriter, kind, id string, revision int) bool {
	if s.scheduler == nil {
		return true
	}
	if _, err := s.scheduler.InvalidateForEntity(kind, id, revision); err != nil {
		s.logger.Error("invalidate changed scheduled publication failed", "kind", kind, "id", id, "revision", revision, "error", err)
		if errors.Is(err, scheduled.ErrTaskRunning) {
			s.writeError(w, http.StatusConflict, "scheduled_publish_running", "The scheduled publication is already running.", nil)
		} else {
			s.writeError(w, http.StatusInternalServerError, "scheduled_publish_invalidate_failed", "The saved content could not update its publication schedule.", nil)
		}
		return false
	}
	return true
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
