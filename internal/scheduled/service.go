package scheduled

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

var (
	ErrTaskNotFound = errors.New("scheduled publish task not found")
	ErrInvalidInput = errors.New("scheduled publish input is invalid")
	ErrTaskRunning  = errors.New("scheduled publish is already running")
)

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
	SchemaVersion     int                `yaml:"schemaVersion" json:"schemaVersion"`
	ID                string             `yaml:"id" json:"id"`
	Kind              string             `yaml:"kind" json:"kind"`
	Operation         string             `yaml:"operation" json:"operation"`
	EntityKind        string             `yaml:"entityKind" json:"entityKind"`
	EntityID          string             `yaml:"entityId" json:"entityId"`
	Revision          int                `yaml:"revision" json:"revision"`
	DueAt             time.Time          `yaml:"dueAt" json:"dueAt"`
	FirstPublish      bool               `yaml:"firstPublish" json:"firstPublish"`
	Status            string             `yaml:"status" json:"status"`
	Progress          taskstore.Progress `yaml:"progress" json:"progress"`
	BuildStatus       string             `yaml:"buildStatus,omitempty" json:"buildStatus,omitempty"`
	BuildTaskID       string             `yaml:"buildTaskId,omitempty" json:"buildTaskId,omitempty"`
	TranslationStatus string             `yaml:"translationStatus,omitempty" json:"translationStatus,omitempty"`
	TranslationTaskID string             `yaml:"translationTaskId,omitempty" json:"translationTaskId,omitempty"`
	Outcome           string             `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	CreatedAt         time.Time          `yaml:"createdAt" json:"createdAt"`
	StartedAt         *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt       *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Error             string             `yaml:"error,omitempty" json:"error,omitempty"`
}

type StartInput struct {
	EntityKind string
	EntityID   string
	Revision   int
	DueAt      time.Time
}

type Rebuilder interface {
	Build(context.Context) (publisher.BuildReport, error)
}

type Translator interface {
	Start(translation.StartInput) (translation.Task, error)
}

type buildTaskReader interface {
	GetTask(string) (publisher.Task, error)
}

type translationTaskReader interface {
	Get(string) (translation.Task, error)
}

type timerHandle struct {
	cancel context.CancelFunc
}

type Service struct {
	repository      *fsrepo.Repository
	content         *content.Service
	rebuilder       Rebuilder
	translator      Translator
	acquire         func() func()
	root            context.Context
	cancel          context.CancelFunc
	semaphore       chan struct{}
	startMu         sync.Mutex
	runMu           sync.Mutex
	lifecycleMu     sync.Mutex
	closed          bool
	writeMu         sync.Mutex
	writeTaskHook   func(Task) error
	terminalMu      sync.Mutex
	terminalClosed  bool
	terminalRetries map[string]terminalRetry
	terminalWG      sync.WaitGroup
	timersMu        sync.Mutex
	timers          map[string]*timerHandle
	wg              sync.WaitGroup
}

func NewService(repository *fsrepo.Repository, contentService *content.Service, rebuilder Rebuilder, translator Translator, acquire func() func()) *Service {
	root, cancel := context.WithCancel(context.Background())
	return &Service{
		repository: repository, content: contentService, rebuilder: rebuilder, translator: translator, acquire: acquire,
		root: root, cancel: cancel, semaphore: make(chan struct{}, 1), timers: map[string]*timerHandle{}, terminalRetries: make(map[string]terminalRetry),
	}
}

func (s *Service) Close() {
	s.lifecycleMu.Lock()
	s.closed = true
	s.lifecycleMu.Unlock()
	s.terminalMu.Lock()
	s.terminalClosed = true
	s.terminalMu.Unlock()
	s.cancel()
	s.wg.Wait()
	s.terminalWG.Wait()
}

func (s *Service) Start(input StartInput) (Task, error) {
	input.DueAt = input.DueAt.UTC()
	if (input.EntityKind != "Post" && input.EntityKind != "Page") || strings.TrimSpace(input.EntityID) == "" || input.Revision < 1 || !input.DueAt.After(time.Now().UTC()) {
		return Task{}, ErrInvalidInput
	}
	item, err := s.getContent(input.EntityKind, input.EntityID)
	if err != nil {
		return Task{}, err
	}
	if item.Meta.Revision != input.Revision {
		return Task{}, content.ErrConflict
	}

	s.startMu.Lock()
	defer s.startMu.Unlock()
	tasks, err := s.List()
	if err != nil {
		return Task{}, err
	}
	for _, existing := range tasks {
		if existing.EntityKind != input.EntityKind || existing.EntityID != input.EntityID || (existing.Status != "queued" && existing.Status != "running") {
			continue
		}
		if existing.Revision == input.Revision && existing.DueAt.Equal(input.DueAt) {
			if _, err := s.content.SetScheduledPublish(input.EntityKind, input.EntityID, input.Revision, input.DueAt); err != nil {
				return Task{}, err
			}
			return existing, nil
		}
		if existing.Status == "running" {
			return Task{}, ErrTaskRunning
		}
	}
	task, err := s.newTask(item, input.Revision, input.DueAt)
	if err != nil {
		return Task{}, err
	}
	for _, existing := range tasks {
		if existing.EntityKind != input.EntityKind || existing.EntityID != input.EntityID || existing.Status != "queued" {
			continue
		}
		s.cancelTimer(existing.ID)
		if err := s.supersedeTask(&existing, "superseded-by-new-schedule"); err != nil {
			return Task{}, err
		}
	}
	if _, err := s.content.SetScheduledPublish(input.EntityKind, input.EntityID, input.Revision, input.DueAt); err != nil {
		return Task{}, err
	}
	if err := s.writeTask(task); err != nil {
		_ = s.content.ClearScheduledPublish(input.EntityKind, input.EntityID, input.Revision)
		return Task{}, err
	}
	if !s.launch(task) {
		slog.Warn("scheduled publish task persisted for next-start recovery because service is closing", "task", task.ID)
	}
	return task, nil
}

func (s *Service) Recover() (int, error) {
	return s.Reconcile()
}

// Reconcile treats content metadata as the durable publication intent and the
// task files as recoverable execution history. It is used both at process
// startup and after a backup restore, whose archive intentionally excludes
// state/tasks.
func (s *Service) Reconcile() (int, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.runMu.Lock()
	defer s.runMu.Unlock()
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	items, err := s.allContent()
	if err != nil {
		return 0, err
	}
	byEntity := make(map[string]domain.Post, len(items))
	for _, item := range items {
		byEntity[entityKey(item.Meta.Kind, item.Meta.ID)] = item
	}
	covered := make(map[string]bool)
	recovered := 0
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		s.cancelTimer(task.ID)
		key := entityKey(task.EntityKind, task.EntityID)
		item, exists := byEntity[key]
		if exists && scheduledHeadCommitted(task, item) {
			repaired, repairErr := s.content.CompleteScheduledPublish(task.EntityKind, task.EntityID, task.Revision, task.DueAt)
			if repairErr != nil {
				return recovered, fmt.Errorf("complete interrupted scheduled publish %q: %w", task.ID, repairErr)
			}
			item = repaired
			byEntity[key] = repaired
		}
		publishedAfterCrash := exists && publishedByScheduledTask(task, item)
		exactIntent := exists && item.Meta.ScheduledRevision == task.Revision && item.Meta.Revision == task.Revision && samePublishTime(item.Meta.PublishedAt, task.DueAt)
		legacyIntent := exists && item.Meta.ScheduledRevision == 0 && item.Meta.Revision == task.Revision && samePublishTime(item.Meta.PublishedAt, task.DueAt)
		if covered[key] {
			if err := s.supersedeTask(&task, "superseded-duplicate-schedule"); err != nil {
				return recovered, err
			}
			continue
		}
		if legacyIntent {
			if _, err := s.content.SetScheduledPublish(task.EntityKind, task.EntityID, task.Revision, task.DueAt); err != nil {
				return recovered, err
			}
			exactIntent = true
		}
		if !exactIntent && !publishedAfterCrash {
			if err := s.finish(&task, "needs-review", "scheduled-publish-intent-changed"); err != nil {
				return recovered, err
			}
			_ = s.content.ClearScheduledPublish(task.EntityKind, task.EntityID, task.Revision)
			if exists && item.Meta.ScheduledRevision == task.Revision {
				covered[key] = true
			}
			continue
		}
		task.Status = "queued"
		task.StartedAt = nil
		task.CompletedAt = nil
		task.Error = ""
		task.Progress = taskstore.Advance(task.Progress, "scheduled", task.Progress.Current, max(4, task.Progress.Total), task.Progress.Percent, "waiting-for-publish-time")
		if err := s.writeTask(task); err != nil {
			if !publishedAfterCrash {
				return recovered, err
			}
			// The public release is the durable execution checkpoint. A stale
			// queued/running task can still be resumed idempotently by run, so a
			// task-file rewrite outage must not suppress the missing static build.
			slog.Error("persist recovered scheduled checkpoint failed after publication; continuing recovery", "task", task.ID, "error", err)
		}
		covered[key] = true
		if !s.launch(task) {
			slog.Warn("scheduled publish recovery stopped before task could launch", "task", task.ID)
		}
		recovered++
	}
	for _, item := range items {
		if item.Meta.ScheduledRevision == 0 {
			continue
		}
		key := entityKey(item.Meta.Kind, item.Meta.ID)
		if covered[key] {
			continue
		}
		dueAt := item.Meta.UpdatedAt.UTC()
		if item.Meta.PublishedAt != nil {
			dueAt = item.Meta.PublishedAt.UTC()
		}
		if dueAt.IsZero() {
			dueAt = time.Now().UTC()
		}
		task, taskErr := s.newTask(item, item.Meta.ScheduledRevision, dueAt)
		if taskErr != nil {
			return recovered, taskErr
		}
		if item.Meta.ScheduledRevision != item.Meta.Revision || item.Meta.PublishedAt == nil {
			now := time.Now().UTC()
			task.Status = "needs-review"
			task.CompletedAt = &now
			task.Error = "content-changed-before-scheduled-publish"
			task.Progress.Message = task.Error
			if err := s.writeTask(task); err != nil {
				return recovered, err
			}
			if err := s.content.ClearScheduledPublish(item.Meta.Kind, item.Meta.ID, item.Meta.ScheduledRevision); err != nil {
				return recovered, err
			}
			continue
		}
		if err := s.writeTask(task); err != nil {
			return recovered, err
		}
		if !s.launch(task) {
			slog.Warn("recreated scheduled publish task persisted for next-start recovery because service is closing", "task", task.ID)
		}
		recovered++
	}
	return recovered, nil
}

// CancelForEntity completes any outstanding schedule before an explicit
// immediate publish proceeds. The server calls this while holding its shared
// mutation gate; scheduled runners require the exclusive side of that gate, so
// cancellation and due-time execution cannot cross in flight.
func (s *Service) CancelForEntity(entityKind, entityID string) (int, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.runMu.Lock()
	defer s.runMu.Unlock()
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	canceled := 0
	for _, task := range tasks {
		if task.EntityKind != entityKind || task.EntityID != entityID || (task.Status != "queued" && task.Status != "running") {
			continue
		}
		if task.Status == "running" {
			return canceled, ErrTaskRunning
		}
		s.cancelTimer(task.ID)
		if err := s.supersedeTask(&task, "superseded-by-immediate-publish"); err != nil {
			return canceled, err
		}
		if err := s.content.ClearScheduledPublish(task.EntityKind, task.EntityID, task.Revision); err != nil {
			return canceled, err
		}
		canceled++
	}
	return canceled, nil
}

// InvalidateForEntity immediately turns a queued schedule into needs-review
// after an editor save changes the optimistic content revision. The persisted
// marker makes the same transition recoverable if the process stops between
// the content write and this call.
func (s *Service) InvalidateForEntity(entityKind, entityID string, currentRevision int) (int, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.runMu.Lock()
	defer s.runMu.Unlock()
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	invalidated := 0
	for _, task := range tasks {
		if task.EntityKind != entityKind || task.EntityID != entityID || task.Revision == currentRevision || (task.Status != "queued" && task.Status != "running") {
			continue
		}
		// A live runner owns runMu for its complete publish/build/translation
		// checkpoint sequence. Reaching this branch with status=running therefore
		// means the record was left behind by a stopped process and is safe to
		// terminalize instead of leaving it stale until its due time.
		s.cancelTimer(task.ID)
		if err := s.finish(&task, "needs-review", "content-changed-before-scheduled-publish"); err != nil {
			return invalidated, err
		}
		if err := s.content.ClearScheduledPublish(task.EntityKind, task.EntityID, task.Revision); err != nil {
			return invalidated, err
		}
		invalidated++
	}
	return invalidated, nil
}

func (s *Service) supersedeTask(task *Task, message string) error {
	now := time.Now().UTC()
	task.Status = "succeeded"
	task.Outcome = "superseded"
	task.Error = ""
	task.CompletedAt = &now
	task.Progress = taskstore.Complete(task.Progress, message)
	return s.writeTask(*task)
}

func (s *Service) List() ([]Task, error) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		return nil, err
	}
	tasks := []Task{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		var task Task
		if err := s.repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return nil, fmt.Errorf("read scheduled publish task %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("scheduled publish task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "ScheduledPublish" {
			continue
		}
		if !validStoredTask(task, expectedID) {
			return nil, fmt.Errorf("scheduled publish task %q is invalid", expectedID)
		}
		normalizeTask(&task)
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks, nil
}

func (s *Service) Get(id string) (Task, error) {
	if !validTaskID(id) {
		return Task{}, ErrTaskNotFound
	}
	var task Task
	if err := s.repository.ReadYAML(filepath.Join("state", "tasks", id+".yaml"), &task); err != nil || !validStoredTask(task, id) {
		return Task{}, ErrTaskNotFound
	}
	normalizeTask(&task)
	return task, nil
}

func (s *Service) launch(task Task) bool {
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(s.root)
	handle := &timerHandle{cancel: cancel}
	s.timersMu.Lock()
	if _, exists := s.timers[task.ID]; exists {
		s.timersMu.Unlock()
		s.lifecycleMu.Unlock()
		cancel()
		return false
	}
	s.timers[task.ID] = handle
	s.timersMu.Unlock()
	s.wg.Add(1)
	s.lifecycleMu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.timersMu.Lock()
			if s.timers[task.ID] == handle {
				delete(s.timers, task.ID)
			}
			s.timersMu.Unlock()
			cancel()
		}()
		delay := time.Until(task.DueAt)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
		select {
		case s.semaphore <- struct{}{}:
			defer func() { <-s.semaphore }()
		case <-ctx.Done():
			return
		}
		s.run(ctx, task.ID)
	}()
	return true
}

func (s *Service) run(ctx context.Context, id string) {
	if s.acquire != nil {
		release := s.acquire()
		defer release()
	}
	if ctx.Err() != nil {
		return
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	task, err := s.Get(id)
	if err != nil || (task.Status != "queued" && task.Status != "running") {
		return
	}
	item, err := s.getContent(task.EntityKind, task.EntityID)
	if err != nil {
		s.finishBestEffort(&task, "failed", "content-unavailable")
		return
	}
	alreadyPublished := publishedByScheduledTask(task, item)
	publicationCommitted := alreadyPublished
	now := time.Now().UTC()
	task.Status = "running"
	task.StartedAt = &now
	task.Progress = taskstore.Advance(task.Progress, "scheduled-publish", task.Progress.Current, 4, 10, "publishing-content-release")
	if !s.checkpointTask(&task, publicationCommitted, "publish-start") {
		return
	}
	if item.Meta.Revision != task.Revision && !alreadyPublished {
		s.finishBestEffort(&task, "needs-review", "content-changed-before-scheduled-publish")
		return
	}
	if !alreadyPublished {
		if _, err := s.publishContent(task.EntityKind, task.EntityID, task.Revision); err != nil {
			status := "failed"
			message := "publish-failed"
			if errors.Is(err, content.ErrConflict) {
				status = "needs-review"
				message = "content-changed-before-scheduled-publish"
			}
			s.finishBestEffort(&task, status, message)
			return
		}
		publicationCommitted = true
	}
	task.Progress = taskstore.Advance(task.Progress, "scheduled-build", 1, 4, 35, "rebuilding-static-site")
	if task.BuildTaskID == "" {
		buildTaskID, buildIDErr := newStaticBuildTaskID()
		if buildIDErr != nil {
			task.BuildStatus = "unavailable"
		} else {
			task.BuildTaskID = buildTaskID
		}
	}
	if !s.checkpointTask(&task, publicationCommitted, "build-start") {
		return
	}
	buildFailure := ""
	if task.BuildStatus == "" {
		if reader, ok := s.rebuilder.(buildTaskReader); ok {
			if buildTask, readErr := reader.GetTask(task.BuildTaskID); readErr == nil {
				switch buildTask.Status {
				case "succeeded":
					task.BuildStatus = "succeeded"
				case "failed":
					if buildTask.Error == "interrupted" {
						buildTaskID, buildIDErr := newStaticBuildTaskID()
						if buildIDErr != nil {
							task.BuildStatus = "unavailable"
						} else {
							task.BuildTaskID = buildTaskID
						}
						if !s.checkpointTask(&task, publicationCommitted, "replace-interrupted-build") {
							return
						}
					} else {
						task.BuildStatus = "failed"
					}
				case "queued", "running":
					// No scheduled runner can still own this child while this
					// service holds runMu. Treat the unfinished child as a crash
					// checkpoint and retry under a fresh collision-free task ID.
					buildTaskID, buildIDErr := newStaticBuildTaskID()
					if buildIDErr != nil {
						task.BuildStatus = "unavailable"
					} else {
						task.BuildTaskID = buildTaskID
					}
					if !s.checkpointTask(&task, publicationCommitted, "replace-unfinished-build") {
						return
					}
				}
			}
		}
		if task.BuildStatus == "" {
			buildContext := publisher.WithBuildRequest(ctx, publisher.BuildRequest{
				TaskID: task.BuildTaskID, Operation: "scheduled-publish", SubjectKind: task.EntityKind, SubjectID: task.EntityID, ParentTaskID: task.ID,
			})
			if s.rebuilder == nil {
				task.BuildStatus = "unavailable"
			} else if _, err := s.rebuilder.Build(buildContext); err != nil {
				task.BuildStatus = "failed"
			} else {
				task.BuildStatus = "succeeded"
			}
		}
		if !s.checkpointTask(&task, publicationCommitted, "build-terminal") {
			return
		}
	}
	if task.BuildStatus == "unavailable" {
		buildFailure = "rebuild-unavailable"
	} else if task.BuildStatus != "succeeded" {
		buildFailure = "rebuild-failed"
	}
	task.Progress = taskstore.Advance(task.Progress, "scheduled-translation", 2, 4, 75, "starting-first-publish-translation")
	if !s.checkpointTask(&task, publicationCommitted, "translation-start") {
		return
	}
	if task.TranslationStatus == "" {
		task.TranslationStatus = "not-needed"
		if task.FirstPublish && s.translator != nil {
			reuseTranslation := false
			if task.TranslationTaskID != "" {
				if reader, ok := s.translator.(translationTaskReader); ok {
					if translationTask, readErr := reader.Get(task.TranslationTaskID); readErr == nil {
						reuseTranslation = true
						switch translationTask.Status {
						case "succeeded":
							task.TranslationStatus = "succeeded"
						case "failed", "needs-review":
							task.TranslationStatus = "failed"
						default:
							task.TranslationStatus = "queued"
						}
					}
				} else {
					reuseTranslation = true
					task.TranslationStatus = "queued"
				}
			}
			if !reuseTranslation {
				translationTask, translationErr := s.translator.Start(translation.StartInput{EntityKind: task.EntityKind, PostID: task.EntityID, SkipManual: true})
				switch {
				case translationErr == nil:
					task.TranslationStatus = "queued"
					task.TranslationTaskID = translationTask.ID
				case errors.Is(translationErr, translation.ErrNoTargets):
					task.TranslationStatus = "not-needed"
				case errors.Is(translationErr, ai.ErrProviderNotFound), errors.Is(translationErr, ai.ErrKeyMissing), errors.Is(translationErr, ai.ErrInvalidProvider):
					task.TranslationStatus = "not-configured"
				default:
					task.TranslationStatus = "failed"
				}
			}
		}
	}
	task.Progress = taskstore.Advance(task.Progress, "scheduled-finalize", 3, 4, 95, "finalizing-scheduled-publish")
	if !s.checkpointTask(&task, publicationCommitted, "finalize") {
		return
	}
	if buildFailure != "" {
		task.Outcome = buildFailure
		// The content release is already durable at this point. A static child
		// failure leaves the previous generated site online, but it must not turn
		// the publication itself into a failed task. Preserve the child status and
		// outcome as a visible warning while completing the parent monotonically.
		s.finishBestEffort(&task, "succeeded", buildFailure)
		return
	}
	s.finishBestEffort(&task, "succeeded", "")
}

func (s *Service) getContent(kind, id string) (domain.Post, error) {
	if kind == "Page" {
		return s.content.GetPage(id)
	}
	return s.content.GetPost(id)
}

func (s *Service) allContent() ([]domain.Post, error) {
	posts, err := s.content.ListPosts()
	if err != nil {
		return nil, err
	}
	pages, err := s.content.ListPages()
	if err != nil {
		return nil, err
	}
	return append(posts, pages...), nil
}

func (s *Service) newTask(item domain.Post, revision int, dueAt time.Time) (Task, error) {
	id, err := newTaskID()
	if err != nil {
		return Task{}, err
	}
	return Task{
		SchemaVersion: domain.SchemaVersion, ID: id, Kind: "ScheduledPublish", Operation: "publish",
		EntityKind: item.Meta.Kind, EntityID: item.Meta.ID, Revision: revision, DueAt: dueAt.UTC(),
		FirstPublish: item.Meta.ReleaseRevision == 0, Status: "queued",
		Progress: taskstore.Progress{Phase: "scheduled", Total: 4, Message: "waiting-for-publish-time"}, CreatedAt: time.Now().UTC(),
	}, nil
}

func entityKey(kind, id string) string {
	return strings.ToLower(strings.TrimSpace(kind)) + ":" + id
}

func samePublishTime(value *time.Time, dueAt time.Time) bool {
	return value != nil && value.UTC().Equal(dueAt.UTC())
}

func scheduledHeadCommitted(task Task, item domain.Post) bool {
	return item.Meta.Status == domain.ContentStatusPublished && item.Meta.Revision == task.Revision+1 && item.Meta.ScheduledRevision == 0 && samePublishTime(item.Meta.PublishedAt, task.DueAt)
}

// publishedByScheduledTask recognizes the durable content effect even if no
// post-publish task checkpoint could be written. PublishPost/Page clears the
// schedule and advances both revision pointers atomically; later AI promotion
// may advance them again. Requiring the cleared intent, scheduled display time,
// and minimum publication revision makes recovery independent of the task file
// without mistaking the pre-schedule public release for completed work.
func publishedByScheduledTask(task Task, item domain.Post) bool {
	publicationRevision := task.Revision + 1
	if item.Meta.Status != domain.ContentStatusPublished || item.Meta.ScheduledRevision != 0 || item.Meta.Revision < publicationRevision || item.Meta.ReleaseRevision < publicationRevision || !samePublishTime(item.Meta.PublishedAt, task.DueAt) {
		return false
	}
	return true
}

func (s *Service) publishContent(kind, id string, revision int) (domain.Post, error) {
	if kind == "Page" {
		return s.content.PublishPage(id, revision)
	}
	return s.content.PublishPost(id, revision)
}

func (s *Service) finish(task *Task, status, message string) error {
	now := time.Now().UTC()
	task.Status = status
	task.CompletedAt = &now
	if status == "succeeded" {
		task.Error = ""
		if task.Outcome == "" {
			task.Outcome = "published"
		}
		completionMessage := "scheduled-publish-complete"
		if message != "" {
			completionMessage = message
		}
		task.Progress = taskstore.Complete(task.Progress, completionMessage)
	} else {
		task.Error = message
		task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, task.Progress.Current, task.Progress.Total, task.Progress.Percent, message)
		if err := s.content.ClearScheduledPublish(task.EntityKind, task.EntityID, task.Revision); err != nil {
			return err
		}
	}
	return s.writeTask(*task)
}

func (s *Service) checkpointTask(task *Task, publicationCommitted bool, checkpoint string) bool {
	err := s.writeTask(*task)
	if err == nil {
		return true
	}
	if publicationCommitted {
		slog.Error("persist scheduled checkpoint failed after publication; continuing task", "task", task.ID, "checkpoint", checkpoint, "error", err)
		return true
	}
	s.finishBestEffort(task, "failed", "progress-write-failed")
	return false
}

func (s *Service) finishBestEffort(task *Task, status, message string) {
	if err := s.finish(task, status, message); err != nil {
		slog.Error("persist terminal scheduled publish task failed", "task", task.ID, "status", status, "error", err)
		s.queueTerminalRetry(*task)
	}
}

func (s *Service) queueTerminalRetry(task Task) {
	s.terminalMu.Lock()
	if s.terminalClosed {
		s.terminalMu.Unlock()
		return
	}
	entry, exists := s.terminalRetries[task.ID]
	entry.task = task
	entry.version++
	s.terminalRetries[task.ID] = entry
	if exists {
		s.terminalMu.Unlock()
		return
	}
	s.terminalWG.Add(1)
	s.terminalMu.Unlock()
	go s.persistTerminalUntilDone(task.ID)
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
		s.terminalMu.Unlock()
		if !exists {
			return
		}
		if err := s.writeTask(entry.task); err != nil {
			slog.Error("retry terminal scheduled publish task persistence failed", "task", taskID, "error", err)
			if delay < terminalRetryMaximumDelay {
				delay *= 2
				if delay > terminalRetryMaximumDelay {
					delay = terminalRetryMaximumDelay
				}
			}
			continue
		}
		s.terminalMu.Lock()
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

func (s *Service) cancelTimer(id string) {
	s.timersMu.Lock()
	handle := s.timers[id]
	delete(s.timers, id)
	s.timersMu.Unlock()
	if handle != nil {
		handle.cancel()
	}
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
			slog.Warn("persist scheduled publish task failed; retrying", "task", task.ID, "attempt", attempt, "error", lastErr)
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *Service) writeTaskOnce(task Task) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeTaskHook != nil {
		if err := s.writeTaskHook(task); err != nil {
			return err
		}
	}
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
}

func validStoredTask(task Task, expectedID string) bool {
	if task.SchemaVersion != domain.SchemaVersion || task.ID != expectedID || task.Kind != "ScheduledPublish" || !validTaskID(task.ID) || (task.EntityKind != "Post" && task.EntityKind != "Page") || task.EntityID == "" || task.Revision < 1 || task.DueAt.IsZero() {
		return false
	}
	if task.BuildTaskID != "" && !taskstore.ValidStaticBuildID(task.BuildTaskID) {
		return false
	}
	switch task.BuildStatus {
	case "", "succeeded", "failed", "unavailable":
	default:
		return false
	}
	if task.TranslationTaskID != "" && !validTranslationTaskID(task.TranslationTaskID) {
		return false
	}
	switch task.TranslationStatus {
	case "", "queued", "not-needed", "not-configured", "failed", "succeeded", "needs-review":
	default:
		return false
	}
	switch task.Status {
	case "queued", "running", "succeeded", "failed", "needs-review":
		return true
	default:
		return false
	}
}

func validTranslationTaskID(id string) bool {
	return strings.HasPrefix(id, "translation-") && len(id) < 128 && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

func normalizeTask(task *Task) {
	if task.Operation == "" {
		task.Operation = "publish"
	}
	if task.Progress.Total < 1 {
		task.Progress.Total = 4
	}
	if task.Progress.Phase == "" {
		task.Progress.Phase = task.Status
	}
	if task.Status == "succeeded" && task.Progress.Percent < 100 {
		task.Progress = taskstore.Complete(task.Progress, "scheduled-publish-complete")
	}
}

func validTaskID(id string) bool {
	if !strings.HasPrefix(id, "scheduled-publish-") || filepath.Base(id) != id || strings.ContainsAny(id, "/\\") {
		return false
	}
	suffix := strings.TrimPrefix(id, "scheduled-publish-")
	return taskstore.ValidStaticBuildID(suffix)
}

func newTaskID() (string, error) {
	id, err := newStaticBuildTaskID()
	if err != nil {
		return "", err
	}
	return "scheduled-publish-" + id, nil
}

func newStaticBuildTaskID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}
