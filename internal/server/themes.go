package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/themes"
)

type saveThemeSettingsRequest struct {
	Values map[string]any `json:"values"`
}

func (s *Server) handleListThemes(w http.ResponseWriter, _ *http.Request) {
	s.themeGate.RLock()
	defer s.themeGate.RUnlock()

	items, err := s.themes.List()
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleThemeScreenshot(w http.ResponseWriter, r *http.Request) {
	s.themeGate.RLock()
	defer s.themeGate.RUnlock()

	path, err := s.themes.Screenshot(r.PathValue("id"))
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		s.writeThemeError(w, themes.ErrNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		s.writeThemeError(w, themes.ErrNotFound)
		return
	}
	contentTypes := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
	}
	w.Header().Set("Content-Type", contentTypes[filepath.Ext(path)])
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleInstallTheme(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, themes.MaxArchiveSize+(1<<20))
	if err := r.ParseMultipartForm(themes.MaxArchiveSize); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "theme_too_large", "The theme package exceeds the 20 MiB upload limit.", nil)
		} else {
			s.writeError(w, http.StatusBadRequest, "multipart_invalid", "The multipart upload is invalid.", nil)
		}
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "theme_missing", "A multipart ZIP field named file is required.", nil)
		return
	}
	defer file.Close()
	if header.Size > themes.MaxArchiveSize {
		s.writeError(w, http.StatusRequestEntityTooLarge, "theme_too_large", "The theme package exceeds the 20 MiB upload limit.", nil)
		return
	}
	s.installThemeReader(w, r, file)
}

func (s *Server) installThemeReader(w http.ResponseWriter, r *http.Request, reader io.Reader) {
	s.themeGate.Lock()
	defer s.themeGate.Unlock()
	build, unlockPublisher := lockPublisherTransaction(s.publisher)
	defer unlockPublisher()

	installation, err := s.themes.BeginInstall(reader)
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	defer installation.Rollback()
	if installation.View.Active && installation.View.Status != "ready" {
		s.writeThemeError(w, themes.ErrIncompatible)
		return
	}
	var report any
	if installation.View.Active {
		buildReport, err := build(r.Context())
		if err != nil {
			s.logger.Warn("active theme upgrade rejected", "theme", installation.View.ID, "error", err)
			s.writeError(w, http.StatusUnprocessableEntity, "theme_render_failed", "The theme could not render the site; the previous theme version is still installed and the public release is unchanged.", nil)
			return
		}
		report = buildReport
	}
	if err := installation.Commit(); err != nil {
		if errors.Is(err, themes.ErrCleanupPending) {
			s.logger.Warn("theme install committed; previous package cleanup deferred", "theme", installation.View.ID, "error", err)
		} else {
			s.writeThemeError(w, err)
			return
		}
	}
	s.recordSecurityEvent(r, "theme-install", "succeeded", adminActor(r))
	s.writeJSON(w, http.StatusCreated, map[string]any{"theme": installation.View, "build": report})
}

func (s *Server) handleActivateTheme(w http.ResponseWriter, r *http.Request) {
	s.themeGate.Lock()
	defer s.themeGate.Unlock()
	build, unlockPublisher := lockPublisherTransaction(s.publisher)
	defer unlockPublisher()

	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeThemeError(w, err)
		return
	}
	previous := site.ActiveTheme
	if previous == "" {
		previous = "earth"
	}
	id := r.PathValue("id")
	if err := s.themes.Activate(id); err != nil {
		s.writeThemeError(w, err)
		return
	}
	report, err := build(r.Context())
	if err != nil {
		if rollbackErr := s.themes.Activate(previous); rollbackErr != nil {
			s.logger.Error("theme activation rollback failed", "theme", id, "previousTheme", previous, "error", rollbackErr)
			s.writeError(w, http.StatusInternalServerError, "theme_rollback_failed", "The theme failed and the previous theme could not be restored.", nil)
			return
		}
		s.logger.Warn("theme activation rejected", "theme", id, "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "theme_render_failed", "The theme could not render the site; the previous theme and public release are still active.", nil)
		return
	}
	s.recordSecurityEvent(r, "theme-activate", "succeeded", adminActor(r))
	s.writeJSON(w, http.StatusOK, map[string]any{"activeTheme": id, "report": report})
}

func (s *Server) handleReloadTheme(w http.ResponseWriter, r *http.Request) {
	s.themeGate.Lock()
	defer s.themeGate.Unlock()
	build, unlockPublisher := lockPublisherTransaction(s.publisher)
	defer unlockPublisher()

	id := r.PathValue("id")
	view, err := s.themeView(id)
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	if view.Status != "ready" {
		s.writeThemeError(w, themes.ErrIncompatible)
		return
	}
	if _, err := s.themes.RuntimeFor(id); err != nil {
		s.writeThemeError(w, err)
		return
	}
	var report any
	if view.Active {
		buildReport, err := build(r.Context())
		if err != nil {
			s.logger.Warn("active theme reload rejected", "theme", id, "error", err)
			s.writeError(w, http.StatusUnprocessableEntity, "theme_render_failed", "The reloaded theme could not render the site; the previous public release remains active.", nil)
			return
		}
		report = buildReport
	}
	s.recordSecurityEvent(r, "theme-reload", "succeeded", adminActor(r))
	s.writeJSON(w, http.StatusOK, map[string]any{"theme": view, "build": report})
}

func (s *Server) handleUninstallTheme(w http.ResponseWriter, r *http.Request) {
	s.themeGate.Lock()
	defer s.themeGate.Unlock()
	_, unlockPublisher := lockPublisherTransaction(s.publisher)
	defer unlockPublisher()

	deleteSettings := r.URL.Query().Get("deleteSettings") == "true"
	if err := s.themes.UninstallWithSettings(r.PathValue("id"), deleteSettings); err != nil {
		if errors.Is(err, themes.ErrCleanupPending) {
			s.logger.Warn("theme uninstall committed; private cleanup deferred", "theme", r.PathValue("id"), "deleteSettings", deleteSettings, "error", err)
		} else {
			s.writeThemeError(w, err)
			return
		}
	}
	action := "theme-uninstall"
	if deleteSettings {
		action = "theme-uninstall-settings"
	}
	s.recordSecurityEvent(r, action, "succeeded", adminActor(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetThemeSettings(w http.ResponseWriter, r *http.Request) {
	s.themeGate.RLock()
	defer s.themeGate.RUnlock()

	view, err := s.themes.Settings(r.PathValue("id"))
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSaveThemeSettings(w http.ResponseWriter, r *http.Request) {
	var request saveThemeSettingsRequest
	if !decodeJSON(w, r, &request) || request.Values == nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	s.updateThemeSettings(w, r, request.Values, false)
}

func (s *Server) handleResetThemeSettings(w http.ResponseWriter, r *http.Request) {
	s.updateThemeSettings(w, r, nil, true)
}

func (s *Server) updateThemeSettings(w http.ResponseWriter, r *http.Request, values map[string]any, reset bool) {
	s.themeGate.Lock()
	defer s.themeGate.Unlock()
	build, unlockPublisher := lockPublisherTransaction(s.publisher)
	defer unlockPublisher()

	id := r.PathValue("id")
	if _, err := s.themes.Settings(id); err != nil {
		s.writeThemeError(w, err)
		return
	}
	settingsFile := filepath.Join("themes", "settings", id+".yaml")
	previous, readErr := s.repository.ReadFile(settingsFile)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		s.writeThemeError(w, readErr)
		return
	}
	var (
		view themes.SettingsView
		err  error
	)
	if reset {
		view, err = s.themes.ResetSettings(id)
	} else {
		view, err = s.themes.SaveSettings(id, values)
	}
	if err != nil {
		s.writeThemeError(w, err)
		return
	}
	var report any
	if view.Active {
		buildReport, buildErr := build(r.Context())
		if buildErr != nil {
			if restoreErr := s.restoreThemeSettings(settingsFile, previous, existed); restoreErr != nil {
				s.logger.Error("theme settings rollback failed", "theme", id, "error", restoreErr)
				s.writeError(w, http.StatusInternalServerError, "theme_settings_rollback_failed", "The settings failed and the previous values could not be restored.", nil)
				return
			}
			s.logger.Warn("theme settings rejected", "theme", id, "error", buildErr)
			s.writeError(w, http.StatusUnprocessableEntity, "theme_render_failed", "The settings could not render the site; the previous values and public release are unchanged.", nil)
			return
		}
		report = buildReport
	}
	s.recordSecurityEvent(r, "theme-settings", "succeeded", adminActor(r))
	s.writeJSON(w, http.StatusOK, map[string]any{"settings": view, "build": report})
}

func (s *Server) restoreThemeSettings(relative string, data []byte, existed bool) error {
	if existed {
		return s.repository.WriteFile(relative, data, 0o640)
	}
	err := s.repository.RemoveFile(relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Server) writeThemeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, themes.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "theme_not_found", "The theme does not exist.", nil)
	case errors.Is(err, themes.ErrActive):
		s.writeError(w, http.StatusConflict, "theme_active", "Activate another theme before uninstalling this one.", nil)
	case errors.Is(err, themes.ErrIncompatible):
		s.writeError(w, http.StatusUnprocessableEntity, "theme_incompatible", "The theme does not support this MutiBlog theme API version.", nil)
	case errors.Is(err, themes.ErrInvalid):
		s.writeError(w, http.StatusUnprocessableEntity, "theme_invalid", "The theme package or identifier is invalid.", nil)
	default:
		s.logger.Error("theme operation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "theme_operation_failed", "The theme operation failed.", nil)
	}
}
