package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

var ErrTaskNotFound = errors.New("backup task not found")

type Task struct {
	SchemaVersion int        `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string     `yaml:"id" json:"id"`
	Kind          string     `yaml:"kind" json:"kind"`
	Operation     string     `yaml:"operation" json:"operation"`
	Status        string     `yaml:"status" json:"status"`
	BackupID      string     `yaml:"backupId,omitempty" json:"backupId,omitempty"`
	Error         string     `yaml:"error,omitempty" json:"error,omitempty"`
	CreatedAt     time.Time  `yaml:"createdAt" json:"createdAt"`
	StartedAt     *time.Time `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt   *time.Time `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
}

func (s *Service) CreateTask(operation string) (Task, error) {
	if operation != "create" && operation != "import-local" && operation != "import-url" && operation != "restore" {
		return Task{}, errors.New("invalid backup task operation")
	}
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return Task{}, err
	}
	task := Task{SchemaVersion: domain.SchemaVersion, ID: "backup-task-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), Kind: "Backup", Operation: operation, Status: "queued", CreatedAt: time.Now().UTC()}
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
	now := time.Now().UTC()
	task.Status = "running"
	task.StartedAt = &now
	task.CompletedAt = nil
	task.Error = ""
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
	now := time.Now().UTC()
	task.Status = status
	task.BackupID = backupID
	task.Error = message
	task.CompletedAt = &now
	if err := s.writeTask(task); err != nil {
		return Task{}, err
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
			if err := s.writeTask(task); err != nil {
				return retry, err
			}
			retry = append(retry, task)
			continue
		}
		if _, err := s.FinishTask(task.ID, "failed", "", "interrupted"); err != nil {
			return retry, err
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
}

func validTaskID(id string) bool {
	return strings.HasPrefix(id, "backup-task-") && len(id) < 128 && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

func validStoredTask(task Task, expectedID string) bool {
	return task.SchemaVersion == domain.SchemaVersion && task.Kind == "Backup" && task.ID == expectedID && validTaskID(task.ID)
}
