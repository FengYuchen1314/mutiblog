package server

import (
	"net/http"
)

// writePublishedResource keeps admin resource responses backwards compatible
// while ensuring resources without a draft lifecycle are reflected in the
// static public release immediately. A failed build never replaces the current
// healthy release; 202 and the response header make that degraded result
// observable without changing the resource JSON shape.
func (s *Server) writePublishedResource(w http.ResponseWriter, r *http.Request, successStatus int, resource any, kind, id string) {
	if _, err := s.publisher.Build(r.Context()); err != nil {
		s.logger.Error("static build after public resource update failed", "kind", kind, "id", id, "error", err)
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, resource)
		return
	}
	w.Header().Set("X-MutiBlog-Static-Build", "succeeded")
	s.writeJSON(w, successStatus, resource)
}

func (s *Server) writePublishedDeletion(w http.ResponseWriter, r *http.Request, kind, id string) {
	if _, err := s.publisher.Build(r.Context()); err != nil {
		s.logger.Error("static build after public resource deletion failed", "kind", kind, "id", id, "error", err)
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, map[string]any{"deleted": true})
		return
	}
	w.Header().Set("X-MutiBlog-Static-Build", "succeeded")
	w.WriteHeader(http.StatusNoContent)
}
