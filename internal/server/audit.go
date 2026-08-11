package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/audit"
)

func (s *Server) recordSecurityEvent(r *http.Request, action, outcome, actor string) {
	digest := sha256.Sum256([]byte(clientIP(r)))
	event := audit.Event{Action: action, Outcome: outcome, Actor: strings.TrimSpace(actor), ClientHash: hex.EncodeToString(digest[:8])}
	if err := s.audit.Record(event); err != nil {
		s.logger.Error("record security audit event failed", "action", action, "outcome", outcome, "error", err)
	}
}

func adminActor(r *http.Request) string {
	current, ok := r.Context().Value(sessionContextKey{}).(sessionContext)
	if !ok {
		return ""
	}
	return current.Session.Username
}

func (s *Server) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.audit.List(limit)
	if err != nil {
		s.logger.Error("read security audit failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "audit_unavailable", "Security audit events are unavailable.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": events})
}
