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
	s.launchBackupRunner(func(ctx context.Context) { s.runBackupCreate(ctx, task.ID) })
	return task, nil
}

func (s *Server) launchBackupRunner(work func(context.Context)) {
	if !s.backups.Launch(work) {
		s.logger.Warn("backup runner persisted for next-start recovery because service is closing")
	}
}

func (s *Server) startBackupTask(ctx context.Context, taskID, operation string) bool {
	starter := s.backupTaskStarter
	if starter == nil {
		starter = s.backups
	}
	delay := 100 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := starter.StartTask(taskID); err == nil {
			return true
		} else if errors.Is(err, backup.ErrTaskNotFound) {
			s.logger.Error("start backup task failed permanently", "task", taskID, "operation", operation, "error", err)
			return false
		} else {
			lastErr = err
			s.logger.Error("start backup task checkpoint failed", "task", taskID, "operation", operation, "attempt", attempt, "error", err)
		}
		if attempt == 3 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
		delay *= 2
	}
	if _, finishErr := s.backups.FinishTask(taskID, "failed", "", "progress-write-failed"); finishErr != nil {
		s.logger.Error("terminalize backup task after start checkpoint exhaustion failed; background retry queued", "task", taskID, "operation", operation, "error", finishErr)
	}
	s.logger.Error("backup task start checkpoint exhausted", "task", taskID, "operation", operation, "error", lastErr)
	return false
}

func (s *Server) runBackupCreate(ctx context.Context, taskID string) {
	if !s.startBackupTask(ctx, taskID, "create") {
		return
	}
	if _, err := s.backups.UpdateTaskProgress(taskID, "backup-snapshot", 1, 4, 20, "capturing-backup-snapshot"); err != nil {
		s.finishBackupTaskFailed(taskID, "progress-write-failed", err)
		return
	}
	s.mutationGate.Lock()
	record, err := s.backups.CreateForTask(taskID)
	s.mutationGate.Unlock()
	if err != nil {
		s.finishBackupTaskFailed(taskID, "create-failed", err)
		return
	}
	if _, err := s.backups.UpdateTaskProgress(taskID, "backup-finalize", 3, 4, 90, "finalizing-backup-archive"); err != nil {
		// The deterministic archive receipt is already durable. Progress is
		// advisory now; terminal success must still be persisted/retried.
		s.logger.Error("persist post-create backup progress failed; continuing terminal success", "task", taskID, "backup", record.ID, "error", err)
	}
	if _, err := s.backups.FinishTask(taskID, "succeeded", record.ID, ""); err != nil {
		s.logger.Error("finish backup create task failed", "task", taskID, "error", err)
	}
}

func (s *Server) runStagedBackupImport(ctx context.Context, taskID string) {
	if !s.startBackupTask(ctx, taskID, "import-local") {
		return
	}
	if _, err := s.backups.UpdateTaskProgress(taskID, "import-validate", 1, 4, 20, "validating-backup-archive"); err != nil {
		s.finishBackupTaskFailed(taskID, "progress-write-failed", err)
		return
	}
	record, err := s.backups.ImportStaged(taskID)
	if err != nil {
		s.finishBackupTaskFailed(taskID, "archive-invalid", err)
		return
	}
	if _, err := s.backups.UpdateTaskProgress(taskID, "import-finalize", 3, 4, 90, "storing-imported-backup"); err != nil {
		s.logger.Error("persist post-import backup progress failed; continuing terminal success", "task", taskID, "backup", record.ID, "error", err)
	}
	if _, err := s.backups.FinishTask(taskID, "succeeded", record.ID, ""); err != nil {
		s.logger.Error("finish local backup import task failed", "task", taskID, "error", err)
	}
}

func (s *Server) runRemoteBackupImport(ctx context.Context, taskID, rawURL string) {
	if !s.startBackupTask(ctx, taskID, "import-url") {
		return
	}
	if _, err := s.backups.UpdateTaskProgress(taskID, "import-download", 1, 4, 15, "downloading-backup-archive"); err != nil {
		s.finishBackupTaskFailed(taskID, "progress-write-failed", err)
		return
	}
	response, err := openPublicHTTPSArchive(ctx, rawURL, backup.MaxImportSize, "application/gzip, application/octet-stream;q=0.9", "MutiBlog-Backup-Importer/1", 5*time.Minute)
	if err != nil {
		s.finishBackupTaskFailed(taskID, "remote-download-failed", errors.New("remote archive request failed"))
		return
	}
	defer response.Body.Close()
	if _, err := s.backups.UpdateTaskProgress(taskID, "import-validate", 2, 4, 45, "validating-downloaded-archive"); err != nil {
		s.finishBackupTaskFailed(taskID, "progress-write-failed", err)
		return
	}
	record, err := s.backups.ImportForTask(taskID, response.Body)
	if err != nil {
		s.finishBackupTaskFailed(taskID, "archive-invalid", err)
		return
	}
	if _, err := s.backups.UpdateTaskProgress(taskID, "import-finalize", 3, 4, 90, "storing-imported-backup"); err != nil {
		s.logger.Error("persist post-remote-import backup progress failed; continuing terminal success", "task", taskID, "backup", record.ID, "error", err)
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
	if _, err := s.backups.UpdateTaskProgress(task.ID, "import-upload", 0, 4, 5, "receiving-backup-upload"); err != nil {
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
	if _, err := s.backups.UpdateTaskProgress(task.ID, "import-upload", 1, 4, 20, "backup-upload-received"); err != nil {
		return err
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
			s.launchBackupRunner(func(ctx context.Context) { s.runBackupCreate(ctx, task.ID) })
		case "import-local":
			s.launchBackupRunner(func(ctx context.Context) { s.runStagedBackupImport(ctx, task.ID) })
		}
	}
}
