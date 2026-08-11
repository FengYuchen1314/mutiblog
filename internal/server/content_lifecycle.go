package server

import (
	"net/http"
)

type contentRevisionRequest struct {
	Revision int `json:"revision"`
}

func (s *Server) handleListPostRevisions(w http.ResponseWriter, r *http.Request) {
	s.handleListContentRevisions(w, r, "post")
}

func (s *Server) handleListPageRevisions(w http.ResponseWriter, r *http.Request) {
	s.handleListContentRevisions(w, r, "page")
}

func (s *Server) handleGetPostRevision(w http.ResponseWriter, r *http.Request) {
	s.handleGetContentRevision(w, r, "post")
}

func (s *Server) handleGetPageRevision(w http.ResponseWriter, r *http.Request) {
	s.handleGetContentRevision(w, r, "page")
}

func (s *Server) handleGetContentRevision(w http.ResponseWriter, r *http.Request, kind string) {
	item, err := s.content.GetRevision(kind, r.PathValue("id"), r.PathValue("revision"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleListContentRevisions(w http.ResponseWriter, r *http.Request, kind string) {
	items, err := s.content.ListRevisions(kind, r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleRestorePostRevision(w http.ResponseWriter, r *http.Request) {
	s.handleRestoreContentRevision(w, r, "post")
}

func (s *Server) handleRestorePageRevision(w http.ResponseWriter, r *http.Request) {
	s.handleRestoreContentRevision(w, r, "page")
}

func (s *Server) handleRestoreContentRevision(w http.ResponseWriter, r *http.Request, kind string) {
	var request contentRevisionRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.content.RestoreRevision(kind, r.PathValue("id"), r.PathValue("revision"), request.Revision)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"post": item, "build": map[string]any{"status": "not-needed"}, "message": "Revision restored as a new head revision; publish explicitly to update the public release."})
}

func (s *Server) handlePostLifecycle(w http.ResponseWriter, r *http.Request) {
	s.handleContentLifecycle(w, r, "post")
}

func (s *Server) handlePageLifecycle(w http.ResponseWriter, r *http.Request) {
	s.handleContentLifecycle(w, r, "page")
}

func (s *Server) handleContentLifecycle(w http.ResponseWriter, r *http.Request, kind string) {
	var request contentRevisionRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.content.ChangeStatus(kind, r.PathValue("id"), r.PathValue("action"), request.Revision)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeContentBuildResult(w, r, item, "Content status updated.")
}

func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	s.handleDeleteContent(w, r, "post")
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	s.handleDeleteContent(w, r, "page")
}

func (s *Server) handleDeleteContent(w http.ResponseWriter, r *http.Request, kind string) {
	var request contentRevisionRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if err := s.content.DeleteRecycled(kind, r.PathValue("id"), request.Revision); err != nil {
		s.writeContentError(w, err)
		return
	}
	if _, err := s.publisher.Build(r.Context()); err != nil {
		s.logger.Error("static build after permanent content deletion failed", "kind", kind, "id", r.PathValue("id"), "error", err)
		s.writeError(w, http.StatusAccepted, "static_build_failed", "The content was deleted, but the previous public release is still active.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeContentBuildResult(w http.ResponseWriter, r *http.Request, item any, message string) {
	report, err := s.publisher.Build(r.Context())
	if err != nil {
		s.logger.Error("static build after content lifecycle operation failed", "error", err)
		s.writeJSON(w, http.StatusAccepted, map[string]any{"post": item, "build": map[string]any{"status": "failed"}, "message": message})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"post": item, "build": map[string]any{"status": "succeeded", "report": report}, "message": message})
}
