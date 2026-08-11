package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

var ErrTaskNotFound = errors.New("backup task not found")

const (
	taskCheckpointWriteAttempts = 3
	terminalRetryInitialDelay   = 100 * time.Millisecond
	terminalRetryMaximumDelay   = 5 * time.Second
)

type terminalRetry struct {
	task    Task
	version uint64
}

type Task struct {
	SchemaVersion int                `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string             `yaml:"id" json:"id"`
	Kind          string             `yaml:"kind" json:"kind"`
	Operation     string             `yaml:"operation" json:"operation"`
	Status        string             `yaml:"status" json:"status"`
	Progress      taskstore.Progress `yaml:"progress" json:"progress"`
	BackupID      string             `yaml:"backupId,omitempty" json:"backupId,omitempty"`
	Error         string             `yaml:"error,omitempty" json:"error,omitempty"`
	CreatedAt     time.Time          `yaml:"createdAt" json:"createdAt"`
	StartedAt     *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt   *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
}

func (s *Service) CreateTask(operation string) (Task, error) {
	return s.CreateTaskWithID(operation, "")
}

// CreateTaskWithID accepts a console-generated ID for long synchronous
// operations such as restore, so the initiating page can poll before the HTTP
// response completes. Empty keeps the server-generated default.
func (s *Service) CreateTaskWithID(operation, requestedID string) (Task, error) {
	if operation != "create" && operation != "import-local" && operation != "import-url" && operation != "restore" {
		return Task{}, errors.New("invalid backup task operation")
	}
	taskID := requestedID
	if taskID == "" {
		random := make([]byte, 4)
		if _, err := rand.Read(random); err != nil {
			return Task{}, err
		}
		taskID = "backup-task-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random)
	} else if !validTaskID(taskID) {
		return Task{}, errors.New("invalid backup task ID")
	}
	exists, err := s.repository.Exists(filepath.Join("state", "tasks", taskID+".yaml"))
	if err != nil {
		return Task{}, err
	}
	if exists {
		return Task{}, errors.New("backup task ID already exists")
	}
	task := Task{SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "Backup", Operation: operation, Status: "queued", Progress: taskstore.Progress{Phase: "queued", Total: 4}, CreatedAt: time.Now().UTC()}
	if err := s.writeTask(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) StartTask(id string) (Task, error) {
	task, err := s.GetTask(id)
	if err != nil {
		return Task{}, err
	}
	if task.Status != "queued" && task.Status != "running" {
		return task, nil
	}
	now := time.Now().UTC()
	task.Status = "running"
	task.StartedAt = &now
	task.CompletedAt = nil
	task.Error = ""
	task.Progress = taskstore.Advance(task.Progress, task.Operation+"-prepare", task.Progress.Current, max(4, task.Progress.Total), 5, "preparing-backup-task")
	if err := s.writeTask(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) UpdateTaskProgress(id, phase string, current, total, percent int, message string) (Task, error) {
	task, err := s.GetTask(id)
	if err != nil {
		return Task{}, err
	}
	if task.Status != "queued" && task.Status != "running" {
		return task, nil
	}
	task.Progress = taskstore.Advance(task.Progress, phase, current, total, percent, message)
	if err := s.writeTask(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) FinishTask(id, status, backupID, message string) (Task, error) {
	if status != "succeeded" && status != "failed" {
		return Task{}, errors.New("invalid backup task status")
	}
	task, err := s.GetTask(id)
	if err != nil {
		return Task{}, err
	}
	if task.Status == "succeeded" || task.Status == "failed" {
		return task, nil
	}
	now := time.Now().UTC()
	task.Status = status
	task.BackupID = backupID
	task.Error = message
	task.CompletedAt = &now
	if status == "succeeded" {
		task.Progress = taskstore.Complete(task.Progress, "backup-task-complete")
	} else {
		task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, task.Progress.Current, task.Progress.Total, task.Progress.Percent, message)
	}
	if err := s.persistTerminal(task); err != nil {
		slog.Error("persist terminal backup task failed", "task", task.ID, "status", status, "error", err)
		return task, err
	}
	return task, nil
}

func (s *Service) ListTasks() ([]Task, error) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		return nil, err
	}
	tasks := []Task{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var task Task
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := s.repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return nil, fmt.Errorf("read backup task %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("backup task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "Backup" {
			continue
		}
		if !validStoredTask(task, expectedID) {
			return nil, fmt.Errorf("backup task %q is invalid", expectedID)
		}
		normalizeLegacyTask(&task)
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks, nil
}

func (s *Service) GetTask(id string) (Task, error) {
	if !validTaskID(id) {
		return Task{}, ErrTaskNotFound
	}
	var task Task
	if err := s.repository.ReadYAML(filepath.Join("state", "tasks", id+".yaml"), &task); err != nil || !validStoredTask(task, id) {
		return Task{}, ErrTaskNotFound
	}
	normalizeLegacyTask(&task)
	return task, nil
}

func (s *Service) RecoverTasks() ([]Task, error) {
	tasks, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	retry := []Task{}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if record, ok := s.taskResult(task.ID); ok {
			if _, finishErr := s.FinishTask(task.ID, "succeeded", record.ID, ""); finishErr != nil {
				slog.Error("persist recovered completed backup task failed; background retry queued", "task", task.ID, "backup", record.ID, "error", finishErr)
			}
			continue
		}
		canRetry := task.Operation == "create"
		if task.Operation == "import-local" {
			_, statErr := os.Stat(s.stagedImportPath(task.ID))
			canRetry = statErr == nil
		}
		if canRetry {
			task.Status = "queued"
			task.StartedAt = nil
			task.CompletedAt = nil
			task.Error = ""
			task.Progress = taskstore.Advance(task.Progress, "queued", task.Progress.Current, max(4, task.Progress.Total), task.Progress.Percent, "resuming-interrupted-task")
			if err := s.writeTask(task); err != nil {
				slog.Error("persist recovered backup checkpoint failed; continuing idempotent runner", "task", task.ID, "error", err)
			}
			retry = append(retry, task)
			continue
		}
		if _, err := s.FinishTask(task.ID, "failed", "", "interrupted"); err != nil {
			slog.Error("persist interrupted backup terminal failed; background retry queued", "task", task.ID, "error", err)
		}
	}
	return retry, nil
}

func (s *Service) StagedImportPath(id string) (string, error) {
	if !validTaskID(id) {
		return "", ErrTaskNotFound
	}
	return s.stagedImportPath(id), nil
}

func (s *Service) stagedImportPath(id string) string {
	return filepath.Join(s.repository.Root(), "state", "tasks", id+".upload")
}

func (s *Service) writeTask(task Task) error {
	if !validTaskID(task.ID) {
		return ErrTaskNotFound
	}
	var lastErr error
	for attempt := 1; attempt <= taskCheckpointWriteAttempts; attempt++ {
		lastErr = s.writeTaskOnce(task)
		if lastErr == nil {
			return nil
		}
		if attempt < taskCheckpointWriteAttempts {
			slog.Warn("persist backup task failed; retrying", "task", task.ID, "attempt", attempt, "error", lastErr)
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *Service) writeTaskOnce(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeTaskHook != nil {
		if err := s.writeTaskHook(task); err != nil {
			return err
		}
	}
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
}

func (s *Service) persistTerminal(task Task) error {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	entry, exists := s.terminalRetries[task.ID]
	entry.task = task
	entry.version++
	if err := s.writeTask(task); err == nil {
		delete(s.terminalRetries, task.ID)
		return nil
	} else if s.terminalClosed {
		return err
	} else {
		s.terminalRetries[task.ID] = entry
		if !exists {
			s.terminalWG.Add(1)
			go s.persistTerminalUntilDone(task.ID)
		}
		return err
	}
}

func (s *Service) persistTerminalUntilDone(taskID string) {
	defer s.terminalWG.Done()
	delay := terminalRetryInitialDelay
	for {
		timer := time.NewTimer(delay)
		select {
		case <-s.root.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.terminalMu.Lock()
		entry, exists := s.terminalRetries[taskID]
		if !exists {
			s.terminalMu.Unlock()
			return
		}
		if err := s.writeTask(entry.task); err != nil {
			s.terminalMu.Unlock()
			slog.Error("retry terminal backup task persistence failed", "task", taskID, "error", err)
			if delay < terminalRetryMaximumDelay {
				delay *= 2
				if delay > terminalRetryMaximumDelay {
					delay = terminalRetryMaximumDelay
				}
			}
			continue
		}
		current, stillPending := s.terminalRetries[taskID]
		if stillPending && current.version == entry.version {
			delete(s.terminalRetries, taskID)
			s.terminalMu.Unlock()
			return
		}
		s.terminalMu.Unlock()
		delay = terminalRetryInitialDelay
	}
}

func (s *Service) taskResult(taskID string) (Record, bool) {
	_, record, err := s.Path(taskResultID(taskID))
	return record, err == nil
}

func taskResultID(taskID string) string {
	digest := sha256.Sum256([]byte(taskID))
	return "backup-task-result-" + hex.EncodeToString(digest[:16])
}

func validTaskID(id string) bool {
	return strings.HasPrefix(id, "backup-task-") && len(id) < 128 && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

func validStoredTask(task Task, expectedID string) bool {
	return task.SchemaVersion == domain.SchemaVersion && task.Kind == "Backup" && task.ID == expectedID && validTaskID(task.ID)
}

func normalizeLegacyTask(task *Task) {
	if task.Progress.Total < 1 {
		task.Progress.Total = 4
	}
	if task.Progress.Phase == "" {
		task.Progress.Phase = task.Status
	}
	if task.Status == "succeeded" && task.Progress.Percent < 100 {
		task.Progress = taskstore.Complete(task.Progress, "backup-task-complete")
	}
}
