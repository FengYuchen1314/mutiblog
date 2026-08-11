package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/comments"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

type createCommentRequest struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Website  string `json:"website"`
	Content  string `json:"content"`
	Locale   string `json:"locale"`
}

type moderateCommentRequest struct {
	Status string `json:"status"`
}

type publicCommentView struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Author   struct {
		Name    string `json:"name"`
		Website string `json:"website,omitempty"`
	} `json:"author"`
	Content   string    `json:"content"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) handlePublicComments(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		var settings domain.CommentsConfig
		if err := s.repository.ReadYAML("config/comments.yaml", &settings); err == nil && settings.PageSize > 0 && settings.PageSize <= 100 {
			size = settings.PageSize
		} else {
			size = 20
		}
	}
	items, total, err := s.comments.ListPublic(r.URL.Query().Get("kind"), r.URL.Query().Get("id"), page, size)
	if err != nil {
		s.writeCommentError(w, err)
		return
	}
	publicItems := make([]publicCommentView, 0, len(items))
	for _, item := range items {
		publicItems = append(publicItems, toPublicCommentView(item))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"page": page, "size": size, "total": total, "items": publicItems})
}

func toPublicCommentView(comment domain.Comment) publicCommentView {
	view := publicCommentView{ID: comment.ID, ParentID: comment.ParentID, Content: comment.Content, Locale: comment.Locale, CreatedAt: comment.CreatedAt}
	view.Author.Name = comment.Author.Name
	view.Author.Website = comment.Author.Website
	return view
}

func (s *Server) handleCreatePublicComment(w http.ResponseWriter, r *http.Request) {
	if s.isThemePreviewHost(r) {
		s.writeError(w, http.StatusForbidden, "preview_read_only", "Theme previews are read-only.", nil)
		return
	}
	var request createCommentRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	ip := clientIP(r)
	comment, err := s.comments.Create(comments.CreateInput{SubjectKind: request.Kind, SubjectID: request.ID, ParentID: request.ParentID, Name: request.Name, Email: request.Email, Website: request.Website, Content: request.Content, Locale: request.Locale, IPAddress: ip, UserAgent: r.UserAgent()})
	if err != nil {
		s.writeCommentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"id": comment.ID, "status": comment.Status})
}

func (s *Server) handleAdminComments(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	items, total, err := s.comments.ListAdmin(comments.AdminQuery{Status: r.URL.Query().Get("status"), Kind: r.URL.Query().Get("kind"), Query: r.URL.Query().Get("q"), Page: page, Size: size})
	if err != nil {
		s.writeCommentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"page": page, "size": size, "items": items, "total": total})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if err := s.comments.Delete(r.PathValue("kind"), r.PathValue("subject"), r.PathValue("id")); err != nil {
		s.writeCommentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleModerateComment(w http.ResponseWriter, r *http.Request) {
	var request moderateCommentRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	comment, err := s.comments.Moderate(r.PathValue("kind"), r.PathValue("subject"), r.PathValue("id"), strings.ToLower(request.Status))
	if err != nil {
		s.writeCommentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, comment)
}

func (s *Server) writeCommentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, comments.ErrInvalid):
		s.writeError(w, http.StatusUnprocessableEntity, "comment_invalid", "The comment fields or subject are invalid.", nil)
	case errors.Is(err, comments.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "comment_not_found", "The comment does not exist.", nil)
	case errors.Is(err, comments.ErrRateLimit):
		s.writeError(w, http.StatusTooManyRequests, "comment_rate_limited", "Too many comments were submitted. Please try again later.", nil)
	default:
		s.logger.Error("comment operation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "comments_unavailable", "The comment service is unavailable.", nil)
	}
}
