package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type protectedConfigFile struct {
	data   []byte
	exists bool
}

func (s *Server) handleListBackups(w http.ResponseWriter, _ *http.Request) {
	items, err := s.backups.List()
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	s.writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) handleCreateBackup(w http.ResponseWriter, _ *http.Request) {
	task, err := s.queueBackupCreate()
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	s.writeJSON(w, http.StatusAccepted, task)
}
func (s *Server) handleImportBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxImportSize+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "backup_upload_too_large", "The backup upload exceeds 512 MiB.", nil)
			return
		}
		s.writeError(w, http.StatusBadRequest, "backup_upload_invalid", "The backup upload is invalid or exceeds 512 MiB.", nil)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "backup_upload_invalid", "A tar.gz backup file no larger than 512 MiB is required.", nil)
		return
	}
	if header.Size > backup.MaxImportSize {
		s.writeError(w, http.StatusRequestEntityTooLarge, "backup_upload_too_large", "The backup upload exceeds 512 MiB.", nil)
		return
	}
	defer file.Close()
	task, err := s.backups.CreateTask("import-local")
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	if err := s.spoolBackupImport(task, file); err != nil {
		s.finishBackupTaskFailed(task.ID, "upload-failed", err)
		if errors.Is(err, backup.ErrInvalidArchive) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "backup_upload_too_large", "The backup upload is empty or exceeds 512 MiB.", nil)
			return
		}
		s.writeBackupError(w, err)
		return
	}
	go s.runStagedBackupImport(task.ID)
	s.recordSecurityEvent(r, "backup-import", "queued", adminActor(r))
	s.writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleImportBackupURL(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if err := validateThemeDownloadURL(request.URL); err != nil {
		s.logger.Warn("remote backup URL rejected")
		s.writeError(w, http.StatusUnprocessableEntity, "backup_download_failed", "The remote backup URL is unsafe.", nil)
		return
	}
	task, err := s.backups.CreateTask("import-url")
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	go s.runRemoteBackupImport(task.ID, request.URL)
	s.recordSecurityEvent(r, "backup-import", "queued", adminActor(r))
	s.writeJSON(w, http.StatusAccepted, task)
}
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	path, record, err := s.backups.Path(r.PathValue("id"))
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+record.DownloadName()+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(w, r, record.DownloadName(), info.ModTime(), file)
}
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if err := s.backups.Delete(r.PathValue("id")); err != nil {
		s.writeBackupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	task, err := s.backups.CreateTask("restore")
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	if _, err := s.backups.StartTask(task.ID); err != nil {
		s.writeBackupError(w, err)
		return
	}
	restoreSucceeded := false
	defer func() {
		if !restoreSucceeded {
			_, _ = s.backups.FinishTask(task.ID, "failed", r.PathValue("id"), "restore-failed")
			s.recordSecurityEvent(r, "backup-restore", "failed", adminActor(r))
		}
	}()
	resumeTranslations := s.translator.Pause()
	defer resumeTranslations()
	safetyBackup, err := s.backups.Create()
	if err != nil {
		s.logger.Error("create pre-restore safety backup failed", "backup", r.PathValue("id"), "error", err)
		s.writeError(w, http.StatusInternalServerError, "backup_safety_copy_failed", "A safety backup could not be created; restore was not started.", nil)
		return
	}
	previousPublicRelease, err := s.capturePublicRelease()
	if err != nil {
		s.logger.Error("capture public release before restore failed", "backup", r.PathValue("id"), "error", err)
		s.writeError(w, http.StatusInternalServerError, "backup_public_pointer_invalid", "The current public release pointer is invalid; restore was not started.", nil)
		return
	}
	protectedConfig, err := s.captureProtectedConfig()
	if err != nil {
		s.logger.Error("capture protected config before restore failed", "backup", r.PathValue("id"), "error", err)
		s.writeError(w, http.StatusInternalServerError, "backup_protected_config_unavailable", "Protected local configuration could not be verified; restore was not started.", nil)
		return
	}
	restoration, err := s.backups.BeginRestoreWithPublicRelease(r.PathValue("id"), previousPublicRelease)
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	defer restoration.Rollback()
	rollback := func() {
		if err := restoration.Rollback(); err != nil {
			s.logger.Error("backup restore rollback failed", "backup", r.PathValue("id"), "error", err)
		}
		if err := s.restorePublicRelease(previousPublicRelease); err != nil {
			s.logger.Error("public release pointer rollback failed", "backup", r.PathValue("id"), "error", err)
		}
		if err := s.projection.Rebuild(); err != nil {
			s.logger.Error("projection rebuild after restore rollback failed", "backup", r.PathValue("id"), "error", err)
		}
	}
	if _, err := s.content.InitializePublishedReleases(); err != nil {
		s.logger.Error("backup restore release migration failed", "backup", r.PathValue("id"), "error", err)
		rollback()
		s.writeError(w, http.StatusUnprocessableEntity, "backup_restore_invalid_releases", "The backup could not initialize public content releases; current data was restored.", nil)
		return
	}
	if err := s.projection.Rebuild(); err != nil {
		s.logger.Error("backup restore index gate rejected data", "backup", r.PathValue("id"), "error", err)
		rollback()
		s.writeError(w, http.StatusUnprocessableEntity, "backup_restore_index_failed", "The backup could not rebuild the search index; current data was restored.", nil)
		return
	}
	report, err := s.publisher.Build(r.Context())
	if err != nil {
		s.logger.Error("backup restore build gate rejected data", "backup", r.PathValue("id"), "error", err)
		rollback()
		s.writeError(w, http.StatusUnprocessableEntity, "backup_restore_build_failed", "The backup could not build the public site; current data and public release were restored.", nil)
		return
	}
	if err := s.pollRestoredHealth(protectedConfig, report); err != nil {
		s.logger.Error("backup restore health gate rejected data", "backup", r.PathValue("id"), "error", err)
		rollback()
		s.writeError(w, http.StatusUnprocessableEntity, "backup_restore_health_failed", "The restored site did not become healthy; current data and public release were restored.", nil)
		return
	}
	if err := restoration.Commit(); err != nil {
		if errors.Is(err, backup.ErrRestoreCommitRolledBack) {
			s.logger.Error("backup restore commit marker failed; restored roots rolled back", "backup", r.PathValue("id"), "error", err)
			if pointerErr := s.restorePublicRelease(previousPublicRelease); pointerErr != nil {
				s.logger.Error("public release pointer rollback after commit failure failed", "backup", r.PathValue("id"), "error", pointerErr)
			}
			if rebuildErr := s.projection.Rebuild(); rebuildErr != nil {
				s.logger.Error("projection rebuild after commit rollback failed", "backup", r.PathValue("id"), "error", rebuildErr)
			}
			s.writeError(w, http.StatusInternalServerError, "backup_restore_commit_failed", "The restored data could not be committed; current data was restored.", nil)
			return
		}
		// Commit has already made the restored roots authoritative; failures
		// here are cleanup-only and must not be reported as a rolled-back restore.
		s.logger.Warn("backup restore committed with cleanup warning", "backup", r.PathValue("id"), "error", err)
	}
	if _, err := s.backups.FinishTask(task.ID, "succeeded", r.PathValue("id"), ""); err != nil {
		s.logger.Error("finish backup restore task failed", "task", task.ID, "error", err)
	}
	restoreSucceeded = true
	s.sessions.Clear()
	s.recordSecurityEvent(r, "backup-restore", "succeeded", adminActor(r))
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "succeeded", "health": "ready", "report": report, "safetyBackup": safetyBackup, "reauthenticate": true})
}

func (s *Server) captureProtectedConfig() (map[string]protectedConfigFile, error) {
	result := make(map[string]protectedConfigFile, 2)
	for _, name := range []string{"secrets.yaml", "initialized"} {
		data, err := s.repository.ReadFile(filepath.Join("config", name))
		if errors.Is(err, os.ErrNotExist) {
			result[name] = protectedConfigFile{}
			continue
		}
		if err != nil {
			return nil, err
		}
		result[name] = protectedConfigFile{data: data, exists: true}
	}
	return result, nil
}

func (s *Server) pollRestoredHealth(protected map[string]protectedConfigFile, report publisher.BuildReport) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if err := s.verifyRestoredHealth(protected, report); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < 4 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *Server) verifyRestoredHealth(protected map[string]protectedConfigFile, report publisher.BuildReport) error {
	for name, expected := range protected {
		data, err := s.repository.ReadFile(filepath.Join("config", name))
		if !expected.exists && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !expected.exists || !bytes.Equal(data, expected.data) {
			return fmt.Errorf("protected config %s changed", name)
		}
	}
	stats := s.projection.Stats()
	if stats.Status != "ready" || stats.LastError != "" {
		return errors.New("search projection is not ready")
	}
	if _, err := s.themes.Runtime(); err != nil {
		return fmt.Errorf("active theme runtime is invalid: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(s.repository.Root(), "generated", "current", "build-report.json"))
	if err != nil {
		return errors.New("public build report is missing")
	}
	var current publisher.BuildReport
	if err := json.Unmarshal(data, &current); err != nil || current.SchemaVersion != report.SchemaVersion || current.GeneratedAt != report.GeneratedAt {
		return errors.New("public build report does not match the restore build")
	}
	return nil
}

func (s *Server) capturePublicRelease() (string, error) {
	current := filepath.Join(s.repository.Root(), "generated", "current")
	target, err := os.Readlink(current)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !validPublicReleaseTarget(target) {
		return "", errors.New("invalid generated/current symlink")
	}
	return target, nil
}

func (s *Server) restorePublicRelease(target string) error {
	generatedRoot := filepath.Join(s.repository.Root(), "generated")
	current := filepath.Join(generatedRoot, "current")
	if target == "" {
		if err := os.Remove(current); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncServerDirectory(generatedRoot)
	}
	if !validPublicReleaseTarget(target) {
		return errors.New("invalid public release rollback target")
	}
	info, err := os.Stat(filepath.Join(generatedRoot, filepath.FromSlash(target)))
	if err != nil || !info.IsDir() {
		return errors.New("public release rollback target is missing")
	}
	temporary := filepath.Join(generatedRoot, fmt.Sprintf(".current-restore-%d", time.Now().UTC().UnixNano()))
	if err := os.Symlink(filepath.FromSlash(target), temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, current); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncServerDirectory(generatedRoot)
}

func validPublicReleaseTarget(target string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	return target == clean && !filepath.IsAbs(target) && strings.HasPrefix(target, "releases/") && clean != "releases" && !strings.Contains(clean, "../")
}

func syncServerDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *Server) writeBackupError(w http.ResponseWriter, err error) {
	if errors.Is(err, backup.ErrNotFound) {
		s.writeError(w, 404, "backup_not_found", "The backup does not exist.", nil)
		return
	}
	if errors.Is(err, backup.ErrInvalidArchive) {
		s.writeError(w, 422, "backup_invalid", "The backup archive is invalid or unsafe.", nil)
		return
	}
	s.logger.Error("backup operation failed", "error", err)
	s.writeError(w, 500, "backup_failed", "The backup operation failed.", nil)
}
