package server

import (
	"net/http"
)

func (s *Server) handleListTranslationTasks(w http.ResponseWriter, _ *http.Request) {
	tasks, err := s.translator.List()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "translation_tasks_unavailable", "Cannot list translation tasks.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": tasks})
}
