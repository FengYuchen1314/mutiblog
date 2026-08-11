package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleIndexStatus(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.projection.Stats())
}

func (s *Server) handleRebuildIndex(w http.ResponseWriter, r *http.Request) {
	// Persist the receipt before returning. The console can then poll a stable
	// ID even if the request is interrupted while the worker is waiting on a
	// concurrent content mutation.
	task, err := s.projection.CreateRebuildTask(strings.TrimSpace(r.Header.Get("X-MutiBlog-Index-Task-ID")))
	if err != nil {
		s.logger.Error("create manual index rebuild task failed", "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "index_rebuild_failed", "The search index rebuild task could not be created.", nil)
		return
	}
	s.launchIndexRebuild(task.ID)
	s.writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
}

func (s *Server) launchIndexRebuild(taskID string) {
	if !s.launchBackground(func(ctx context.Context) { s.runIndexRebuild(ctx, taskID) }) {
		s.finishIndexRebuildTask(taskID, "interrupted", context.Canceled)
	}
}

func (s *Server) runIndexRebuild(ctx context.Context, taskID string) {
	if _, err := s.projection.StartRebuildTask(taskID); err != nil {
		s.logger.Error("start search index rebuild task failed", "task", taskID, "error", err)
		// A task receipt already exists at this point. Do not leave it queued
		// forever when its first running checkpoint cannot be persisted; a
		// best-effort terminal record gives Task Center an actionable outcome.
		s.finishIndexRebuildTask(taskID, "progress-write-failed", err)
		return
	}
	if err := ctx.Err(); err != nil {
		s.finishIndexRebuildTask(taskID, "interrupted", err)
		return
	}
	if _, err := s.projection.UpdateRebuildTaskProgress(taskID, "index-rebuild", 1, 3, 25, "rebuilding-search-index"); err != nil {
		s.finishIndexRebuildTask(taskID, "progress-write-failed", err)
		return
	}
	s.mutationGate.RLock()
	err := s.projection.Rebuild()
	s.mutationGate.RUnlock()
	if err != nil {
		s.finishIndexRebuildTask(taskID, "index-rebuild-failed", err)
		return
	}
	if err := ctx.Err(); err != nil {
		s.finishIndexRebuildTask(taskID, "interrupted", err)
		return
	}
	if _, err := s.projection.UpdateRebuildTaskProgress(taskID, "index-finalize", 2, 3, 90, "finalizing-search-index"); err != nil {
		// The projection has already committed successfully. Preserve that fact in
		// logs, then make a best-effort terminal result instead of replacing it
		// with a false index failure.
		s.logger.Error("persist search index rebuild final checkpoint failed", "task", taskID, "error", err)
	}
	if _, err := s.projection.FinishRebuildTask(taskID, "succeeded", ""); err != nil {
		s.logger.Error("finish search index rebuild task failed", "task", taskID, "error", err)
	}
}

func (s *Server) finishIndexRebuildTask(taskID, code string, cause error) {
	if cause != nil && !errors.Is(cause, context.Canceled) {
		s.logger.Error("search index rebuild task failed", "task", taskID, "error", cause)
	}
	if _, err := s.projection.FinishRebuildTask(taskID, "failed", code); err != nil {
		s.logger.Error("persist failed search index rebuild task failed", "task", taskID, "error", err)
	}
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
