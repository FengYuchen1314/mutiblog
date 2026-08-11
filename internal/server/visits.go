package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/visits"
)

type createVisitRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type publicProfileStats struct {
	Posts             int   `json:"posts"`
	Categories        int   `json:"categories"`
	Comments          *int  `json:"comments"`
	Visits            int64 `json:"visits"`
	CommentsAvailable bool  `json:"commentsAvailable"`
}

type publicStatsResponse struct {
	Locale       string               `json:"locale"`
	PopularPosts []visits.PopularPost `json:"popularPosts"`
	Profile      publicProfileStats   `json:"profile"`
}

type cachedPublicStats struct {
	response  publicStatsResponse
	expiresAt time.Time
}

const (
	publicStatsCacheTTL  = 15 * time.Second
	maxPublicStatsCaches = 64
)

func (s *Server) clearPublicStatsCache() {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	s.statsCache = make(map[string]cachedPublicStats)
}

func (s *Server) handleCreatePublicVisit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.isThemePreviewHost(r) {
		s.writeError(w, http.StatusForbidden, "preview_read_only", "Theme previews are read-only.", nil)
		return
	}
	var request createVisitRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	result, err := s.visits.Record(request.Kind, request.ID, clientIP(r))
	if err != nil {
		s.writeVisitError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handlePublicStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	locale, err := s.visits.EnabledLocale(r.URL.Query().Get("locale"))
	if err != nil {
		s.writeVisitError(w, err)
		return
	}
	response, err := s.cachedPublicStats(locale)
	if err != nil {
		s.writeVisitError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, response)
}

// cachedPublicStats serializes cache misses so a burst for one or many
// locales cannot repeat filesystem-wide content/comment scans concurrently.
// Only validated enabled locale keys enter the small, absolute-bounded cache.
func (s *Server) cachedPublicStats(locale string) (publicStatsResponse, error) {
	now := time.Now()
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	for key, cached := range s.statsCache {
		if !cached.expiresAt.After(now) {
			delete(s.statsCache, key)
		}
	}
	if cached, ok := s.statsCache[locale]; ok {
		return cached.response, nil
	}
	snapshot, err := s.visits.PublicSnapshot(locale, 5)
	if err != nil {
		return publicStatsResponse{}, err
	}
	profile := publicProfileStats{
		Posts:      snapshot.Profile.Posts,
		Categories: snapshot.Profile.Categories,
		Visits:     snapshot.Profile.Visits,
	}
	if s.comments != nil {
		items, commentErr := s.comments.ListAll()
		if commentErr != nil {
			s.logger.Warn("public profile comment statistics unavailable", "error", commentErr)
		} else {
			approved := 0
			for _, comment := range items {
				if comment.Status != "approved" {
					continue
				}
				if _, public := snapshot.PublicSubjects[visits.SubjectKey(comment.Subject.Kind, comment.Subject.ID)]; public {
					approved++
				}
			}
			profile.Comments = &approved
			profile.CommentsAvailable = true
		}
	}
	response := publicStatsResponse{Locale: snapshot.Locale, PopularPosts: snapshot.PopularPosts, Profile: profile}
	if s.statsCache == nil {
		s.statsCache = make(map[string]cachedPublicStats)
	}
	if len(s.statsCache) < maxPublicStatsCaches {
		s.statsCache[locale] = cachedPublicStats{response: response, expiresAt: now.Add(publicStatsCacheTTL)}
	}
	return response, nil
}

func (s *Server) writeVisitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, visits.ErrInvalid):
		s.writeError(w, http.StatusUnprocessableEntity, "visit_invalid", "The visit subject or locale is invalid.", nil)
	case errors.Is(err, visits.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "visit_not_found", "The published content does not exist.", nil)
	case errors.Is(err, visits.ErrRateLimit):
		w.Header().Set("Retry-After", "1800")
		s.writeError(w, http.StatusTooManyRequests, "visit_rate_limited", "This visit was not counted again yet.", nil)
	default:
		s.logger.Error("visit statistics operation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "statistics_unavailable", "Visit statistics are unavailable.", nil)
	}
}
