package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/backup"
)

func (s *Server) handleListBackupTasks(w http.ResponseWriter, _ *http.Request) {
	tasks, err := s.backups.ListTasks()
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": tasks})
}

func (s *Server) queueBackupCreate() (backup.Task, error) {
	task, err := s.backups.CreateTask("create")
	if err != nil {
		return backup.Task{}, err
	}
	go s.runBackupCreate(task.ID)
	return task, nil
}

func (s *Server) runBackupCreate(taskID string) {
	s.mutationGate.Lock()
	defer s.mutationGate.Unlock()
	if _, err := s.backups.StartTask(taskID); err != nil {
		s.logger.Error("start backup create task failed", "task", taskID, "error", err)
		return
	}
	record, err := s.backups.Create()
	if err != nil {
		s.finishBackupTaskFailed(taskID, "create-failed", err)
		return
	}
	if _, err := s.backups.FinishTask(taskID, "succeeded", record.ID, ""); err != nil {
		s.logger.Error("finish backup create task failed", "task", taskID, "error", err)
	}
}

func (s *Server) runStagedBackupImport(taskID string) {
	if _, err := s.backups.StartTask(taskID); err != nil {
		s.logger.Error("start local backup import task failed", "task", taskID, "error", err)
		return
	}
	record, err := s.backups.ImportStaged(taskID)
	if err != nil {
		s.finishBackupTaskFailed(taskID, "archive-invalid", err)
		return
	}
	if _, err := s.backups.FinishTask(taskID, "succeeded", record.ID, ""); err != nil {
		s.logger.Error("finish local backup import task failed", "task", taskID, "error", err)
	}
}

func (s *Server) runRemoteBackupImport(taskID, rawURL string) {
	if _, err := s.backups.StartTask(taskID); err != nil {
		s.logger.Error("start remote backup import task failed", "task", taskID, "error", err)
		return
	}
	response, err := openPublicHTTPSArchive(context.Background(), rawURL, backup.MaxImportSize, "application/gzip, application/octet-stream;q=0.9", "MutiBlog-Backup-Importer/1", 5*time.Minute)
	if err != nil {
		s.finishBackupTaskFailed(taskID, "remote-download-failed", errors.New("remote archive request failed"))
		return
	}
	defer response.Body.Close()
	record, err := s.backups.Import(response.Body)
	if err != nil {
		s.finishBackupTaskFailed(taskID, "archive-invalid", err)
		return
	}
	if _, err := s.backups.FinishTask(taskID, "succeeded", record.ID, ""); err != nil {
		s.logger.Error("finish remote backup import task failed", "task", taskID, "error", err)
	}
}

func (s *Server) spoolBackupImport(task backup.Task, reader io.Reader) error {
	path, err := s.backups.StagedImportPath(task.ID)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, backup.MaxImportSize+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(path)
		return copyErr
	}
	if syncErr != nil {
		os.Remove(path)
		return syncErr
	}
	if closeErr != nil {
		os.Remove(path)
		return closeErr
	}
	if written == 0 || written > backup.MaxImportSize {
		os.Remove(path)
		return backup.ErrInvalidArchive
	}
	return nil
}

func (s *Server) finishBackupTaskFailed(taskID, message string, cause error) {
	s.logger.Error("backup task failed", "task", taskID, "error", cause)
	if _, err := s.backups.FinishTask(taskID, "failed", "", message); err != nil {
		s.logger.Error("persist backup task failure failed", "task", taskID, "error", err)
	}
}

func (s *Server) resumeBackupTasks(tasks []backup.Task) {
	for _, task := range tasks {
		switch task.Operation {
		case "create":
			go s.runBackupCreate(task.ID)
		case "import-local":
			go s.runStagedBackupImport(task.ID)
		}
	}
}
