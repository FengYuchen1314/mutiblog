package projection

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

var ErrTaskNotFound = errors.New("index rebuild task not found")

const (
	indexRebuildOperation       = "rebuild-search-index"
	indexTaskCheckpointAttempts = 3
)

// Task is the durable receipt for a manually requested search-index rebuild.
// The projection itself remains derived state; the task only records the
// operator-visible work and survives a browser refresh or process restart.
type Task struct {
	SchemaVersion int                `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string             `yaml:"id" json:"id"`
	Kind          string             `yaml:"kind" json:"kind"`
	Operation     string             `yaml:"operation" json:"operation"`
	Status        string             `yaml:"status" json:"status"`
	Progress      taskstore.Progress `yaml:"progress" json:"progress"`
	Error         string             `yaml:"error,omitempty" json:"error,omitempty"`
	CreatedAt     time.Time          `yaml:"createdAt" json:"createdAt"`
	StartedAt     *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt   *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
}

// NewRebuildTaskID makes a collision-resistant server-side task ID for API
// clients that did not provide their own correlation ID.
func NewRebuildTaskID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "index-rebuild-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

// CreateRebuildTask writes the queued receipt before work begins. A caller may
// supply a browser-generated ID so the console can begin polling immediately.
func (s *Service) CreateRebuildTask(requestedID string) (Task, error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	taskID := strings.TrimSpace(requestedID)
	if taskID == "" {
		var err error
		taskID, err = NewRebuildTaskID()
		if err != nil {
			return Task{}, err
		}
	} else if !taskstore.ValidIndexRebuildID(taskID) {
		return Task{}, errors.New("index rebuild task ID is invalid")
	}
	exists, err := s.repository.Exists(indexTaskPath(taskID))
	if err != nil {
		return Task{}, err
	}
	if exists {
		return Task{}, errors.New("index rebuild task ID already exists")
	}
	task := Task{
		SchemaVersion: domain.SchemaVersion,
		ID:            taskID,
		Kind:          "IndexRebuild",
		Operation:     indexRebuildOperation,
		Status:        "queued",
		Progress:      taskstore.Progress{Phase: "queued", Total: 3, Message: "waiting-to-rebuild-search-index"},
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.writeTaskLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// StartRebuildTask records that the worker has claimed a queued task. It is
// idempotent so a runner may safely retry after a transient checkpoint error.
func (s *Service) StartRebuildTask(id string) (Task, error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	task, err := s.readTaskLocked(id)
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
	task.Progress = taskstore.Advance(task.Progress, "index-prepare", 0, 3, 5, "preparing-search-index")
	if err := s.writeTaskLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// UpdateRebuildTaskProgress records a monotonic checkpoint. Terminal tasks
// remain immutable so delayed workers cannot regress a completed receipt.
func (s *Service) UpdateRebuildTaskProgress(id, phase string, current, total, percent int, message string) (Task, error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	task, err := s.readTaskLocked(id)
	if err != nil {
		return Task{}, err
	}
	if task.Status != "queued" && task.Status != "running" {
		return task, nil
	}
	task.Progress = taskstore.Advance(task.Progress, phase, current, total, percent, message)
	if err := s.writeTaskLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// FinishRebuildTask persists a terminal result. Failures use stable error
// codes, not raw filesystem or SQLite details, because the task is surfaced in
// the administrative console.
func (s *Service) FinishRebuildTask(id, status, errorCode string) (Task, error) {
	if status != "succeeded" && status != "failed" {
		return Task{}, errors.New("index rebuild task terminal status is invalid")
	}
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	task, err := s.readTaskLocked(id)
	if err != nil {
		return Task{}, err
	}
	if task.Status == "succeeded" || task.Status == "failed" {
		return task, nil
	}
	now := time.Now().UTC()
	task.Status = status
	task.CompletedAt = &now
	if status == "succeeded" {
		task.Error = ""
		task.Progress = taskstore.Complete(task.Progress, "search-index-rebuild-complete")
	} else {
		if errorCode == "" {
			errorCode = "index-rebuild-failed"
		}
		task.Error = errorCode
		task.Progress = taskstore.Advance(task.Progress, "index-failed", task.Progress.Current, task.Progress.Total, task.Progress.Percent, errorCode)
	}
	if err := s.writeTaskLocked(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) GetTask(id string) (Task, error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	return s.readTaskLocked(id)
}

func (s *Service) ListTasks() ([]Task, error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	return s.listTasksLocked()
}

// RecoverTasks makes an interrupted manual rebuild explicit. Opening the
// projection already performs a fresh index rebuild, but it cannot prove that
// a particular administrator request reached its intended checkpoint.
func (s *Service) RecoverTasks() error {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	tasks, err := s.listTasksLocked()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		now := time.Now().UTC()
		task.Status = "failed"
		task.CompletedAt = &now
		task.Error = "interrupted"
		task.Progress = taskstore.Advance(task.Progress, "index-failed", task.Progress.Current, task.Progress.Total, task.Progress.Percent, "interrupted")
		if err := s.writeTaskLocked(task); err != nil {
			return fmt.Errorf("persist interrupted index rebuild task %q: %w", task.ID, err)
		}
	}
	return nil
}

func (s *Service) listTasksLocked() ([]Task, error) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		var task Task
		if err := s.repository.ReadYAML(indexTaskPath(expectedID), &task); err != nil {
			return nil, fmt.Errorf("read index rebuild task %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("index rebuild task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "IndexRebuild" {
			continue
		}
		if !validStoredTask(task, expectedID) {
			return nil, fmt.Errorf("index rebuild task %q is invalid", expectedID)
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks, nil
}

func (s *Service) readTaskLocked(id string) (Task, error) {
	if !taskstore.ValidIndexRebuildID(id) {
		return Task{}, ErrTaskNotFound
	}
	var task Task
	if err := s.repository.ReadYAML(indexTaskPath(id), &task); err != nil || !validStoredTask(task, id) {
		return Task{}, ErrTaskNotFound
	}
	return task, nil
}

func (s *Service) writeTaskLocked(task Task) error {
	if !validStoredTask(task, task.ID) {
		return ErrTaskNotFound
	}
	var lastErr error
	for attempt := 1; attempt <= indexTaskCheckpointAttempts; attempt++ {
		lastErr = s.repository.WriteYAML(indexTaskPath(task.ID), task, false)
		if lastErr == nil {
			return nil
		}
		if attempt < indexTaskCheckpointAttempts {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
	}
	return lastErr
}

func indexTaskPath(id string) string {
	return filepath.Join("state", "tasks", id+".yaml")
}

func validStoredTask(task Task, expectedID string) bool {
	if task.SchemaVersion != domain.SchemaVersion || task.ID != expectedID || task.Kind != "IndexRebuild" || task.Operation != indexRebuildOperation || !taskstore.ValidIndexRebuildID(task.ID) {
		return false
	}
	switch task.Status {
	case "queued", "running", "succeeded", "failed":
		return true
	default:
		return false
	}
}
