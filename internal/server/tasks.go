package server

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
	"github.com/FengYuchen1314/mutiblog/internal/projection"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/scheduled"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type adminTaskSubject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type adminTask struct {
	SchemaVersion     int                    `json:"schemaVersion"`
	ID                string                 `json:"id"`
	Kind              string                 `json:"kind"`
	Operation         string                 `json:"operation"`
	Subject           *adminTaskSubject      `json:"subject,omitempty"`
	ParentTaskID      string                 `json:"parentTaskId,omitempty"`
	Status            string                 `json:"status"`
	Progress          taskstore.Progress     `json:"progress"`
	CreatedAt         time.Time              `json:"createdAt"`
	StartedAt         *time.Time             `json:"startedAt,omitempty"`
	CompletedAt       *time.Time             `json:"completedAt,omitempty"`
	Error             string                 `json:"error,omitempty"`
	ProviderID        string                 `json:"providerId,omitempty"`
	Model             string                 `json:"model,omitempty"`
	BackupID          string                 `json:"backupId,omitempty"`
	DueAt             *time.Time             `json:"dueAt,omitempty"`
	BuildStatus       string                 `json:"buildStatus,omitempty"`
	BuildTaskID       string                 `json:"buildTaskId,omitempty"`
	TranslationStatus string                 `json:"translationStatus,omitempty"`
	TranslationTaskID string                 `json:"translationTaskId,omitempty"`
	Outcome           string                 `json:"outcome,omitempty"`
	Targets           any                    `json:"targets,omitempty"`
	Report            *publisher.BuildReport `json:"report,omitempty"`
}

type staticTaskReader interface {
	ListTasks() ([]publisher.Task, error)
	GetTask(string) (publisher.Task, error)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.listAdminTasks()
	if err != nil {
		s.logger.Error("list durable tasks failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "tasks_unavailable", "Task history is unavailable because a durable task record is invalid or unreadable.", nil)
		return
	}
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	filtered := items[:0]
	for _, item := range items {
		if kindFilter != "" && !strings.EqualFold(item.Kind, kindFilter) {
			continue
		}
		if statusFilter != "" && !strings.EqualFold(item.Status, statusFilter) {
			continue
		}
		filtered = append(filtered, item)
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = min(parsed, 500)
		}
	}
	total := len(filtered)
	active := 0
	for _, item := range filtered {
		if item.Status == "queued" || item.Status == "running" {
			active++
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": filtered, "total": total, "active": active})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var item adminTask
	var err error
	switch {
	case strings.HasPrefix(id, "translation-"):
		var task translation.Task
		task, err = s.translator.Get(id)
		if err == nil {
			item = adminTaskFromTranslation(task)
		}
	case strings.HasPrefix(id, "backup-task-"):
		var task backup.Task
		task, err = s.backups.GetTask(id)
		if err == nil {
			item = adminTaskFromBackup(task)
		}
	case strings.HasPrefix(id, "scheduled-publish-"):
		var task scheduled.Task
		if s.scheduler == nil {
			err = scheduled.ErrTaskNotFound
			break
		}
		task, err = s.scheduler.Get(id)
		if err == nil {
			item = adminTaskFromScheduledPublish(task)
		}
	case taskstore.ValidLocaleProvisionID(id):
		if s.localeTasks == nil {
			err = localization.ErrLocaleProvisionTaskNotFound
			break
		}
		var task localization.Task
		task, err = s.localeTasks.Get(id)
		if err == nil {
			item = adminTaskFromLocaleProvision(task)
		}
	case taskstore.ValidIndexRebuildID(id):
		if s.projection == nil {
			err = projection.ErrTaskNotFound
			break
		}
		var task projection.Task
		task, err = s.projection.GetTask(id)
		if err == nil {
			item = adminTaskFromIndexRebuild(task)
		}
	case taskstore.ValidStaticBuildID(id):
		reader, ok := s.publisher.(staticTaskReader)
		if !ok {
			err = publisher.ErrBuildTaskNotFound
			break
		}
		var task publisher.Task
		task, err = reader.GetTask(id)
		if err == nil {
			item = adminTaskFromStaticBuild(task)
		}
	default:
		err = errors.New("task not found")
	}
	if err != nil {
		s.writeError(w, http.StatusNotFound, "task_not_found", "The task does not exist.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) listAdminTasks() ([]adminTask, error) {
	translations, err := s.translator.List()
	if err != nil {
		return nil, err
	}
	backups, err := s.backups.ListTasks()
	if err != nil {
		return nil, err
	}
	scheduledTasks := []scheduled.Task{}
	if s.scheduler != nil {
		scheduledTasks, err = s.scheduler.List()
		if err != nil {
			return nil, err
		}
	}
	builds := []publisher.Task{}
	if reader, ok := s.publisher.(staticTaskReader); ok {
		builds, err = reader.ListTasks()
		if err != nil {
			return nil, err
		}
	}
	indexRebuilds := []projection.Task{}
	if s.projection != nil {
		indexRebuilds, err = s.projection.ListTasks()
		if err != nil {
			return nil, err
		}
	}
	localeProvisions := []localization.Task{}
	if s.localeTasks != nil {
		localeProvisions, err = s.localeTasks.List()
		if err != nil {
			return nil, err
		}
	}
	items := make([]adminTask, 0, len(translations)+len(backups)+len(builds)+len(scheduledTasks)+len(indexRebuilds)+len(localeProvisions))
	for _, task := range translations {
		items = append(items, adminTaskFromTranslation(task))
	}
	for _, task := range builds {
		items = append(items, adminTaskFromStaticBuild(task))
	}
	for _, task := range backups {
		items = append(items, adminTaskFromBackup(task))
	}
	for _, task := range scheduledTasks {
		items = append(items, adminTaskFromScheduledPublish(task))
	}
	for _, task := range indexRebuilds {
		items = append(items, adminTaskFromIndexRebuild(task))
	}
	for _, task := range localeProvisions {
		items = append(items, adminTaskFromLocaleProvision(task))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func adminTaskFromTranslation(task translation.Task) adminTask {
	operation := "translate"
	if task.PublicationRevision > 0 {
		operation = "publish-translate"
	}
	return adminTask{
		SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind, Operation: operation,
		Subject: &adminTaskSubject{Kind: task.EntityKind, ID: task.EntityID}, Status: task.Status,
		Progress: task.Progress, CreatedAt: task.CreatedAt, StartedAt: task.StartedAt, CompletedAt: task.CompletedAt,
		Error: task.Error, ProviderID: task.ProviderID, Model: task.Model, Targets: task.Targets,
		BuildStatus: task.BuildStatus, BuildTaskID: task.BuildTaskID,
	}
}

func adminTaskFromStaticBuild(task publisher.Task) adminTask {
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = task.StartedAt
	}
	var startedAt *time.Time
	if !task.StartedAt.IsZero() {
		value := task.StartedAt
		startedAt = &value
	}
	var subject *adminTaskSubject
	if task.SubjectKind != "" || task.SubjectID != "" {
		subject = &adminTaskSubject{Kind: task.SubjectKind, ID: task.SubjectID}
	}
	return adminTask{
		SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind, Operation: task.Operation,
		Subject: subject, ParentTaskID: task.ParentTaskID, Status: task.Status, Progress: task.Progress,
		CreatedAt: createdAt, StartedAt: startedAt, CompletedAt: task.CompletedAt, Error: task.Error, Report: task.Report,
	}
}

func adminTaskFromBackup(task backup.Task) adminTask {
	return adminTask{
		SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind, Operation: task.Operation,
		Subject: backupTaskSubject(task), Status: task.Status, Progress: task.Progress, CreatedAt: task.CreatedAt,
		StartedAt: task.StartedAt, CompletedAt: task.CompletedAt, Error: task.Error, BackupID: task.BackupID,
	}
}

func adminTaskFromIndexRebuild(task projection.Task) adminTask {
	return adminTask{
		SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind, Operation: task.Operation,
		Status: task.Status, Progress: task.Progress, CreatedAt: task.CreatedAt,
		StartedAt: task.StartedAt, CompletedAt: task.CompletedAt, Error: task.Error,
	}
}

func adminTaskFromLocaleProvision(task localization.Task) adminTask {
	return adminTask{
		SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind, Operation: task.Operation,
		Status: task.Status, Progress: task.Progress, CreatedAt: task.CreatedAt,
		StartedAt: task.StartedAt, CompletedAt: task.CompletedAt, Error: task.Error,
		BuildStatus: task.BuildStatus, BuildTaskID: task.BuildTaskID, Targets: task.Targets,
	}
}

func adminTaskFromScheduledPublish(task scheduled.Task) adminTask {
	dueAt := task.DueAt
	return adminTask{
		SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind, Operation: task.Operation,
		Subject: &adminTaskSubject{Kind: task.EntityKind, ID: task.EntityID}, Status: task.Status, Progress: task.Progress,
		CreatedAt: task.CreatedAt, StartedAt: task.StartedAt, CompletedAt: task.CompletedAt, Error: task.Error,
		DueAt: &dueAt, BuildStatus: task.BuildStatus, BuildTaskID: task.BuildTaskID,
		TranslationStatus: task.TranslationStatus, TranslationTaskID: task.TranslationTaskID,
		Outcome: task.Outcome,
	}
}

func backupTaskSubject(task backup.Task) *adminTaskSubject {
	if task.BackupID == "" {
		return nil
	}
	return &adminTaskSubject{Kind: "Backup", ID: task.BackupID}
}

// withBuildTaskRequest attaches a browser-generated static task ID before any
// handler starts a synchronous publish/rebuild. The trigger page can poll that
// ID immediately, while the durable publisher still owns validation and state.
func (s *Server) withBuildTaskRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := publisher.BuildRequest{TaskID: strings.TrimSpace(r.Header.Get("X-MutiBlog-Task-ID"))}
		request.Operation, request.SubjectKind, request.SubjectID = describeBuildRequest(r)
		next.ServeHTTP(w, r.WithContext(publisher.WithBuildRequest(r.Context(), request)))
	})
}

func describeBuildRequest(r *http.Request) (operation, subjectKind, subjectID string) {
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case path == "api/v1/admin/publish/build":
		operation = "manual-rebuild"
	case strings.HasSuffix(path, "/publish"):
		operation = "publish"
	case strings.Contains(path, "/backups/") && strings.HasSuffix(path, "/restore"):
		operation = "backup-restore"
	case strings.Contains(path, "/themes/") || strings.Contains(path, "/themes"):
		operation = "theme-rebuild"
	case strings.Contains(path, "/settings/"):
		operation = "settings-rebuild"
	case strings.Contains(path, "/locales") || strings.Contains(path, "/dictionaries"):
		operation = "localization-rebuild"
	default:
		operation = "content-rebuild"
	}
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if index+1 >= len(parts) {
			continue
		}
		switch part {
		case "posts":
			return operation, "Post", parts[index+1]
		case "pages":
			return operation, "Page", parts[index+1]
		case "themes":
			return operation, "Theme", parts[index+1]
		case "backups":
			return operation, "Backup", parts[index+1]
		}
	}
	return operation, "", ""
}
