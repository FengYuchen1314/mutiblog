package server

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleIndexStatus(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.projection.Stats())
}

func (s *Server) handleRebuildIndex(w http.ResponseWriter, _ *http.Request) {
	if err := s.projection.Rebuild(); err != nil {
		s.logger.Error("manual projection rebuild failed", "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "index_rebuild_failed", "The file truth source contains invalid data and the index could not be rebuilt.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, s.projection.Stats())
}

func (s *Server) handleIndexSearch(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.projection.Search(strings.TrimSpace(r.URL.Query().Get("q")), limit)
	if err != nil {
		s.logger.Error("projection search failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "index_search_failed", "The search index is unavailable.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}
