package translation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"golang.org/x/text/language"
)

var (
	ErrNoTargets          = errors.New("no translation targets")
	ErrManualConfirmation = errors.New("manual translations require confirmation")
	ErrTaskNotFound       = errors.New("translation task not found")
)

const (
	translationTargetTimeout    = 15 * time.Minute
	taskCheckpointWriteAttempts = 3
	terminalRetryInitialDelay   = 100 * time.Millisecond
	terminalRetryMaximumDelay   = 5 * time.Second
)

type terminalRetry struct {
	task    Task
	version uint64
}

type Rebuilder interface {
	Build(context.Context) (publisher.BuildReport, error)
}

type TargetTask struct {
	Locale           string             `yaml:"locale" json:"locale"`
	ExpectedRevision int                `yaml:"expectedRevision,omitempty" json:"expectedRevision"`
	ExpectedContent  string             `yaml:"expectedContent,omitempty" json:"expectedContent,omitempty"`
	ResultContent    string             `yaml:"resultContent,omitempty" json:"resultContent,omitempty"`
	Status           string             `yaml:"status" json:"status"`
	Progress         taskstore.Progress `yaml:"progress" json:"progress"`
	Attempts         int                `yaml:"attempts" json:"attempts"`
	Error            string             `yaml:"error,omitempty" json:"error,omitempty"`
	CompletedAt      *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
}

type Task struct {
	SchemaVersion   int                `yaml:"schemaVersion" json:"schemaVersion"`
	ID              string             `yaml:"id" json:"id"`
	Kind            string             `yaml:"kind" json:"kind"`
	EntityKind      string             `yaml:"entityKind" json:"entityKind"`
	EntityID        string             `yaml:"entityId" json:"entityId"`
	SourceLocale    string             `yaml:"sourceLocale" json:"sourceLocale"`
	SourceRevision  int                `yaml:"sourceRevision" json:"sourceRevision"`
	ProviderID      string             `yaml:"providerId" json:"providerId"`
	Model           string             `yaml:"model" json:"model"`
	OverwriteManual bool               `yaml:"overwriteManual" json:"overwriteManual"`
	SourceContent   string             `yaml:"sourceContent,omitempty" json:"sourceContent,omitempty"`
	Status          string             `yaml:"status" json:"status"`
	Progress        taskstore.Progress `yaml:"progress" json:"progress"`
	Targets         []TargetTask       `yaml:"targets" json:"targets"`
	CreatedAt       time.Time          `yaml:"createdAt" json:"createdAt"`
	StartedAt       *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt     *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Error           string             `yaml:"error,omitempty" json:"error,omitempty"`
}

type StartInput struct {
	EntityKind      string
	PostID          string
	Locales         []string
	OverwriteManual bool
	SkipManual      bool
	// RecordPreflightFailure opts automatic publication flows into durable,
	// sanitized task history when a real target exists but AI is not configured.
	RecordPreflightFailure bool
}

type Service struct {
	repository      *fsrepo.Repository
	content         *content.Service
	ai              *ai.Service
	rebuilder       Rebuilder
	semaphore       chan struct{}
	startMu         sync.Mutex
	mu              sync.Mutex
	callbackMu      sync.RWMutex
	contentChanged  func(entityKind, entityID string, revision int)
	mutationAcquire func() func()
	runGate         sync.RWMutex
	writeTaskHook   func(Task) error
	root            context.Context
	cancel          context.CancelFunc
	lifecycleMu     sync.Mutex
	closed          bool
	launched        map[string]struct{}
	runWG           sync.WaitGroup
	terminalMu      sync.Mutex
	terminalClosed  bool
	terminalRetries map[string]terminalRetry
	terminalWG      sync.WaitGroup
	generation      atomic.Uint64
}

func NewService(repository *fsrepo.Repository, contentService *content.Service, aiService *ai.Service, rebuilder Rebuilder) *Service {
	root, cancel := context.WithCancel(context.Background())
	service := &Service{
		repository: repository, content: contentService, ai: aiService, rebuilder: rebuilder, semaphore: make(chan struct{}, 1),
		root: root, cancel: cancel, launched: make(map[string]struct{}), terminalRetries: make(map[string]terminalRetry),
	}
	service.generation.Store(1)
	return service
}

// Close cancels provider/build work, waits for runners to leave all content and
// publisher callbacks, then waits for terminal checkpoint persistence workers.
// Interrupted queued/running records remain recoverable on the next start.
func (s *Service) Close() {
	s.lifecycleMu.Lock()
	s.closed = true
	s.lifecycleMu.Unlock()
	s.cancel()
	s.runWG.Wait()
	s.terminalMu.Lock()
	s.terminalClosed = true
	s.terminalMu.Unlock()
	s.terminalWG.Wait()
}

func (s *Service) launch(taskID string) bool {
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return false
	}
	if _, exists := s.launched[taskID]; exists {
		s.lifecycleMu.Unlock()
		return false
	}
	s.launched[taskID] = struct{}{}
	s.runWG.Add(1)
	s.lifecycleMu.Unlock()
	go func() {
		defer s.runWG.Done()
		s.run(taskID)
	}()
	return true
}

// SetContentChangedCallback installs the post-commit hook used by the
// scheduled-publish service. The callback runs only after an AI locale was
// durably applied (and public promotion was attempted); merely queuing or
// failing provider work must not cancel a publication schedule.
func (s *Service) SetContentChangedCallback(callback func(entityKind, entityID string, revision int)) {
	s.callbackMu.Lock()
	s.contentChanged = callback
	s.callbackMu.Unlock()
}

// SetContentMutationAcquire installs an optional narrow mutation gate. Provider
// calls intentionally remain outside it; only the durable Apply/Promote pair
// is serialized with other content mutations and external-change projection
// handling. The resulting schedule invalidation callback runs after this gate
// is released, while the pause gate still prevents a restore from interleaving.
func (s *Service) SetContentMutationAcquire(acquire func() func()) {
	s.callbackMu.Lock()
	s.mutationAcquire = acquire
	s.callbackMu.Unlock()
}

func (s *Service) acquireContentMutation() func() {
	s.callbackMu.RLock()
	acquire := s.mutationAcquire
	s.callbackMu.RUnlock()
	if acquire == nil {
		return func() {}
	}
	release := acquire()
	if release == nil {
		return func() {}
	}
	return release
}

func (s *Service) withDurableContentMutation(work, after func()) {
	release := s.acquireContentMutation()
	var releaseOnce sync.Once
	releaseMutation := func() { releaseOnce.Do(release) }
	// Resolve and acquire the mutation gate before entering the pause gate to
	// preserve the server-wide lock order. sync.Once makes both the normal and
	// panic paths safe without risking a double unlock.
	s.runGate.RLock()
	defer s.runGate.RUnlock()
	defer releaseMutation()
	work()
	releaseMutation()
	if after != nil {
		after()
	}
}

func (s *Service) withPauseGate(work func()) {
	s.runGate.RLock()
	defer s.runGate.RUnlock()
	work()
}

func (s *Service) notifyContentChanged(entityKind, entityID string, revision int) {
	s.callbackMu.RLock()
	callback := s.contentChanged
	s.callbackMu.RUnlock()
	if callback != nil {
		callback(entityKind, entityID, revision)
	}
}

// Recover requeues translation work that was interrupted by a process stop.
// Already-applied AI translations are detected from their source revision, so
// recovery does not spend provider quota translating the same revision twice.
func (s *Service) Recover() (int, error) {
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if !taskHasContentIdentity(task) {
			// Tasks written before content identities were introduced cannot be
			// proven to belong to the current repository after a backup restore.
			// Conservatively require review instead of allowing revision-number
			// collisions to apply an old provider response to restored content.
			if err := s.finishTask(&task, "needs-review", "content-identity-unavailable"); err != nil {
				slog.Error("persist legacy translation terminal failed; background retry queued", "task", task.ID, "error", err)
			}
			recovered++
			continue
		}
		task.Status = "queued"
		task.StartedAt = nil
		task.CompletedAt = nil
		task.Error = ""
		for index := range task.Targets {
			if task.Targets[index].Status == "running" {
				task.Targets[index].Status = "queued"
				task.Targets[index].CompletedAt = nil
				task.Targets[index].Error = ""
			}
		}
		if err := s.writeTask(task); err != nil {
			// The stored queued/running record is itself enough for run to resume
			// idempotently. Do not strand recoverable work merely because the
			// normalization checkpoint could not be rewritten in this process.
			slog.Error("persist recovered translation checkpoint failed; continuing recovery", "task", task.ID, "error", err)
		}
		recovered++
		if !s.launch(task.ID) {
			slog.Warn("translation recovery stopped before task could launch", "task", task.ID)
		}
	}
	return recovered, nil
}

func (s *Service) Start(input StartInput) (Task, error) {
	task, created, err := s.Prepare(input)
	if err != nil {
		return task, err
	}
	if created && !s.LaunchPrepared(task.ID) {
		slog.Warn("translation task persisted for next-start recovery because service is closing", "task", task.ID)
	}
	return task, nil
}

// Prepare validates a translation request and durably records new work without
// starting provider calls. Reusing an identical queued/running task returns
// created=false so its existing owner remains solely responsible for launch.
func (s *Service) Prepare(input StartInput) (Task, bool, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	entityKind := input.EntityKind
	if entityKind == "" {
		entityKind = "Post"
	}
	post, err := s.getContent(entityKind, input.PostID)
	if err != nil {
		return Task{}, false, err
	}
	targets, manual, err := s.targets(post, input.Locales)
	if err != nil {
		return Task{}, false, err
	}
	if len(manual) > 0 && !input.OverwriteManual {
		if input.SkipManual {
			manualSet := make(map[string]bool, len(manual))
			for _, locale := range manual {
				manualSet[locale] = true
			}
			filtered := targets[:0]
			for _, locale := range targets {
				if !manualSet[locale] {
					filtered = append(filtered, locale)
				}
			}
			targets = filtered
			if len(targets) == 0 {
				return Task{}, false, ErrNoTargets
			}
		} else {
			return Task{Targets: toTargetTasks(manual)}, false, ErrManualConfirmation
		}
	}
	provider, _, err := s.ai.DefaultCredentials()
	if err != nil {
		if input.RecordPreflightFailure && recordableProviderPreflightError(err) {
			task, recordErr := s.recordPreflightFailure(post, entityKind, targets, input.OverwriteManual, err)
			if recordErr != nil {
				return Task{}, false, errors.Join(err, fmt.Errorf("persist translation preflight failure: %w", recordErr))
			}
			return task, false, err
		}
		return Task{}, false, err
	}
	if existing, ok, err := s.activeTask(post, provider.ID, input.OverwriteManual, targets); err != nil {
		return Task{}, false, err
	} else if ok {
		return existing, false, nil
	}
	taskID, err := newTaskID()
	if err != nil {
		return Task{}, false, err
	}
	task := Task{
		SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "Translation", EntityKind: entityKind, EntityID: post.Meta.ID,
		SourceLocale: post.Meta.SourceLocale, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		ProviderID: provider.ID, Model: provider.Model, OverwriteManual: input.OverwriteManual,
		Status: "queued", Targets: toTargetTasks(targets, post.Meta.Locales), CreatedAt: time.Now().UTC(),
	}
	initializeTranslationIdentity(&task, post)
	initializeTranslationProgress(&task, post.Content[post.Meta.SourceLocale], provider.MaxOutputTokens)
	if err := s.writeTask(task); err != nil {
		return Task{}, false, err
	}
	return task, true, nil
}

func recordableProviderPreflightError(err error) bool {
	return errors.Is(err, ai.ErrProviderNotFound) || errors.Is(err, ai.ErrKeyMissing) || errors.Is(err, ai.ErrInvalidProvider)
}

func (s *Service) recordPreflightFailure(post domain.Post, entityKind string, targets []string, overwriteManual bool, cause error) (Task, error) {
	taskID, err := newTaskID()
	if err != nil {
		return Task{}, err
	}
	now := time.Now().UTC()
	safeError := safeTaskError(cause)
	task := Task{
		SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "Translation", EntityKind: entityKind, EntityID: post.Meta.ID,
		SourceLocale: post.Meta.SourceLocale, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		OverwriteManual: overwriteManual, Status: "failed", Targets: toTargetTasks(targets, post.Meta.Locales),
		CreatedAt: now, CompletedAt: &now, Error: safeError,
	}
	initializeTranslationIdentity(&task, post)
	for index := range task.Targets {
		task.Targets[index].Status = "failed"
		task.Targets[index].Error = safeError
		task.Targets[index].CompletedAt = &now
		task.Targets[index].Progress = taskstore.Advance(task.Targets[index].Progress, "translation-preflight", 0, 1, 0, safeError)
	}
	task.Progress = taskstore.Advance(task.Progress, "translation-preflight", 0, len(task.Targets)+1, 0, safeError)
	if err := s.writeTask(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// LaunchPrepared starts a durable translation ID at most once in this service
// process. run performs the authoritative stored-status check; avoiding a
// second pre-launch read prevents a transient read failure immediately after a
// successful Prepare write from stranding the task until process restart.
func (s *Service) LaunchPrepared(taskID string) bool {
	if !validTaskID(taskID) {
		return false
	}
	return s.launch(taskID)
}

// activeTask prevents duplicate clicks, repeated publish requests, and client
// retries from spending provider quota on identical in-flight work. Completed
// tasks are deliberately excluded so an administrator can explicitly run the
// same translation again after reviewing the result.
func (s *Service) activeTask(post domain.Post, providerID string, overwriteManual bool, targets []string) (Task, bool, error) {
	tasks, err := s.List()
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if task.EntityKind != post.Meta.Kind || task.EntityID != post.Meta.ID || task.SourceLocale != post.Meta.SourceLocale || task.SourceRevision != post.Meta.Locales[post.Meta.SourceLocale].Revision || task.ProviderID != providerID || task.OverwriteManual != overwriteManual {
			continue
		}
		if sameTargets(task.Targets, targets) && taskMatchesSource(task, post) && taskMatchesCurrentTargets(task, post) {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func taskMatchesCurrentTargets(task Task, post domain.Post) bool {
	for _, target := range task.Targets {
		localized, exists := post.Content[target.Locale]
		// ResultContent is written before ApplyAITranslation. Accepting either
		// identity keeps a partially committed active task reusable, while rejecting
		// stale tasks whose revision numbers merely collide after backup restore.
		if !targetMatchesExpected(target, localized, exists) && !targetMatchesResult(target, localized, exists) {
			return false
		}
	}
	return true
}

func sameTargets(existing []TargetTask, requested []string) bool {
	if len(existing) != len(requested) {
		return false
	}
	for index := range existing {
		if existing[index].Locale != requested[index] {
			return false
		}
	}
	return true
}

func (s *Service) List() ([]Task, error) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var task Task
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := s.repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return nil, fmt.Errorf("read translation task %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("translation task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "Translation" {
			continue
		}
		if !validStoredTask(task, expectedID) {
			return nil, fmt.Errorf("translation task %q is invalid", expectedID)
		}
		normalizeLegacyTranslationTask(&task)
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
	if err := s.repository.ReadYAML(filepath.Join("state", "tasks", id+".yaml"), &task); err != nil {
		return Task{}, ErrTaskNotFound
	}
	if !validStoredTask(task, id) {
		return Task{}, ErrTaskNotFound
	}
	normalizeLegacyTranslationTask(&task)
	return task, nil
}

func (s *Service) targets(post domain.Post, requested []string) ([]string, []string, error) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return nil, nil, err
	}
	enabled := make(map[string]bool)
	for _, locale := range config.Enabled {
		if locale.Enabled && locale.Code != post.Meta.SourceLocale {
			enabled[locale.Code] = true
		}
	}
	if len(requested) == 0 {
		for locale := range enabled {
			requested = append(requested, locale)
		}
	}
	unique := make(map[string]bool)
	var targets, manual []string
	for _, raw := range requested {
		tag, err := language.Parse(raw)
		if err != nil || !enabled[tag.String()] {
			return nil, nil, content.ErrLocaleDisabled
		}
		locale := tag.String()
		if unique[locale] {
			continue
		}
		unique[locale] = true
		targets = append(targets, locale)
		if state, exists := post.Meta.Locales[locale]; exists && state.Origin == domain.LocaleOriginManual {
			manual = append(manual, locale)
		}
	}
	if len(targets) == 0 {
		return nil, nil, ErrNoTargets
	}
	sort.Strings(targets)
	sort.Strings(manual)
	return targets, manual, nil
}

func (s *Service) run(taskID string) {
	// Do not hold the pause gate across provider network calls. A backup restore
	// already owns the server mutation lock before it calls Pause; holding this
	// read lock while later waiting for that mutation lock would deadlock. This
	// short barrier prevents new work from entering while a restore is active.
	s.withPauseGate(func() {})
	if s.root.Err() != nil {
		return
	}
	select {
	case s.semaphore <- struct{}{}:
	case <-s.root.Done():
		return
	}
	defer func() { <-s.semaphore }()
	task, err := s.Get(taskID)
	if err != nil || (task.Status != "queued" && task.Status != "running") {
		return
	}
	runGeneration := s.generation.Load()
	post, err := s.getContent(task.EntityKind, task.EntityID)
	if err != nil {
		s.finishTask(&task, "needs-review", "source-changed-before-start")
		return
	}
	if !taskMatchesSource(task, post) {
		s.finishTask(&task, "needs-review", "source-changed-before-start")
		return
	}
	hasDurableMutation := false
	for _, target := range task.Targets {
		state, exists := post.Meta.Locales[target.Locale]
		if target.Status == "succeeded" || (exists && state.Origin == domain.LocaleOriginAI && state.State == "current" && state.SourceRevision == task.SourceRevision) {
			hasDurableMutation = true
			break
		}
	}
	started := time.Now().UTC()
	task.Status = "running"
	task.StartedAt = &started
	task.Progress = taskstore.Advance(task.Progress, "translation-validate", task.Progress.Current, task.Progress.Total, 1, "validating-source")
	if !s.checkpointTask(&task, hasDurableMutation, "start") {
		return
	}
	if post.Meta.Locales[post.Meta.SourceLocale].Revision != task.SourceRevision {
		s.finishTask(&task, "needs-review", "source-changed-before-start")
		return
	}
	source := post.Content[post.Meta.SourceLocale]
	initializeTranslationProgress(&task, source)
	task.Progress = taskstore.Advance(task.Progress, "translation-target", task.Progress.Current, task.Progress.Total, 2, "preparing-targets")
	if !s.checkpointTask(&task, hasDurableMutation, "prepare-targets") {
		return
	}
	failed := false
	needsReview := false
	hasSuccessfulTarget := false
	for index := range task.Targets {
		if s.root.Err() != nil {
			return
		}
		if !s.generationCurrent(runGeneration) {
			s.finishTask(&task, "needs-review", "source-changed-before-start")
			return
		}
		post, postErr := s.getContent(task.EntityKind, task.EntityID)
		if postErr != nil {
			failed = true
			completed := time.Now().UTC()
			task.Targets[index].Status = "failed"
			task.Targets[index].Error = safeTaskError(postErr)
			task.Targets[index].CompletedAt = &completed
			if !s.checkpointTask(&task, hasDurableMutation, "target-content-unavailable") {
				return
			}
			continue
		}
		targetContent, targetExists := post.Content[task.Targets[index].Locale]
		targetState, targetStateExists := post.Meta.Locales[task.Targets[index].Locale]
		resultMatches := targetMatchesResult(task.Targets[index], targetContent, targetExists)
		if task.Targets[index].Status == "succeeded" {
			if !targetStateExists || targetState.Origin != domain.LocaleOriginAI || targetState.State != "current" || targetState.SourceRevision != task.SourceRevision || !resultMatches {
				failed = true
				needsReview = true
				completed := time.Now().UTC()
				task.Targets[index].Status = "needs-review"
				task.Targets[index].Error = safeTaskError(content.ErrTargetChanged)
				task.Targets[index].CompletedAt = &completed
				if !s.checkpointTask(&task, hasDurableMutation, "completed-target-identity-mismatch") {
					return
				}
				continue
			}
			task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
			hasSuccessfulTarget = true
			hasDurableMutation = true
			continue
		}
		if targetStateExists && targetState.Origin == domain.LocaleOriginAI && targetState.State == "current" && targetState.SourceRevision == task.SourceRevision {
			if !resultMatches {
				failed = true
				needsReview = true
				completed := time.Now().UTC()
				task.Targets[index].Status = "needs-review"
				task.Targets[index].Error = safeTaskError(content.ErrTargetChanged)
				task.Targets[index].CompletedAt = &completed
				if !s.checkpointTask(&task, hasDurableMutation, "recovered-target-identity-mismatch") {
					return
				}
				continue
			}
			// The AI head is already durable, even if public promotion below
			// fails. From this point checkpoint I/O must not prevent sibling
			// targets or the public rebuild from being attempted.
			hasDurableMutation = true
			var promoteErr error
			notifyChange := false
			s.withDurableContentMutation(func() {
				if !s.generationCurrent(runGeneration) {
					promoteErr = content.ErrSourceChanged
					return
				}
				promoteErr = s.content.PromoteAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, task.SourceRevision)
				// The AI head mutation is already durable in this recovery branch even
				// if public promotion now fails, so its old schedule must still retire.
				notifyChange = true
			}, func() {
				if notifyChange {
					s.notifyContentChanged(task.EntityKind, task.EntityID, post.Meta.Revision)
				}
			})
			if promoteErr != nil {
				failed = true
				task.Targets[index].Status = taskStatusForError(promoteErr)
				needsReview = needsReview || task.Targets[index].Status == "needs-review"
				task.Targets[index].Error = safeTaskError(promoteErr)
				if !s.checkpointTask(&task, hasDurableMutation, "recovered-target-promotion-failed") {
					return
				}
				continue
			}
			completed := time.Now().UTC()
			task.Targets[index].Status = "succeeded"
			task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
			hasSuccessfulTarget = true
			task.Targets[index].Error = ""
			task.Targets[index].CompletedAt = &completed
			if !s.checkpointTask(&task, hasDurableMutation, "recovered-target-complete") {
				return
			}
			continue
		}
		currentRevision := 0
		if targetStateExists {
			currentRevision = targetState.Revision
		}
		if currentRevision != task.Targets[index].ExpectedRevision || !targetMatchesExpected(task.Targets[index], targetContent, targetExists) {
			failed = true
			needsReview = true
			completed := time.Now().UTC()
			task.Targets[index].Status = "needs-review"
			task.Targets[index].Error = safeTaskError(content.ErrTargetChanged)
			task.Targets[index].CompletedAt = &completed
			if !s.checkpointTask(&task, hasDurableMutation, "target-needs-review") {
				return
			}
			continue
		}
		task.Targets[index].Status = "running"
		task.Targets[index].Attempts++
		if !s.checkpointResult(&task, s.updateTargetProgress(&task, index, "metadata", 0, max(2, task.Targets[index].Progress.Total), "translating-metadata"), hasDurableMutation, "target-start") {
			return
		}
		targetContext, cancelTarget := context.WithTimeout(s.root, translationTargetTimeout)
		translated, translateErr := s.translate(targetContext, task.ProviderID, task.SourceLocale, task.Targets[index].Locale, source, func(update translationProgressUpdate) error {
			progressErr := s.updateTargetProgress(&task, index, update.Phase, update.Current, update.Total, update.Message)
			if progressErr != nil && hasDurableMutation {
				slog.Error("persist translation progress failed after durable content mutation; continuing provider work", "task", task.ID, "target", task.Targets[index].Locale, "phase", update.Phase, "error", progressErr)
				return nil
			}
			return progressErr
		})
		cancelTarget()
		if s.root.Err() != nil {
			return
		}
		if !s.generationCurrent(runGeneration) {
			s.finishTask(&task, "needs-review", "source-changed-before-start")
			return
		}
		if translateErr == nil {
			translated = normalizeTranslationResult(translated)
			task.Targets[index].ResultContent = localizedContentFingerprint(translated, true)
			total := max(2, task.Targets[index].Progress.Total)
			if !s.checkpointResult(&task, s.updateTargetProgress(&task, index, "save", total-1, total, "saving-translation"), hasDurableMutation, "target-ready-to-save") {
				return
			}
			expectedTargetRevision := task.Targets[index].ExpectedRevision
			var updated domain.Post
			applied := false
			s.withDurableContentMutation(func() {
				if !s.generationCurrent(runGeneration) {
					translateErr = content.ErrSourceChanged
					return
				}
				latest, latestErr := s.getContent(task.EntityKind, task.EntityID)
				if latestErr != nil || !taskMatchesSource(task, latest) {
					translateErr = content.ErrSourceChanged
					return
				}
				latestTarget, latestTargetExists := latest.Content[task.Targets[index].Locale]
				if !targetMatchesExpected(task.Targets[index], latestTarget, latestTargetExists) {
					translateErr = content.ErrTargetChanged
					return
				}
				updated, translateErr = s.applyAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, content.ApplyAITranslationInput{
					ExpectedSourceRevision: task.SourceRevision, ExpectedTargetRevision: &expectedTargetRevision, OverwriteManual: task.OverwriteManual, Content: translated,
				})
				applied = translateErr == nil
				if applied {
					translateErr = s.content.PromoteAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, task.SourceRevision)
				}
			}, func() {
				if applied {
					s.notifyContentChanged(task.EntityKind, task.EntityID, updated.Meta.Revision)
				}
			})
			if applied {
				hasDurableMutation = true
			}
		}
		completed := time.Now().UTC()
		task.Targets[index].CompletedAt = &completed
		if translateErr != nil {
			failed = true
			task.Targets[index].Status = taskStatusForError(translateErr)
			needsReview = needsReview || task.Targets[index].Status == "needs-review"
			task.Targets[index].Error = safeTaskError(translateErr)
		} else {
			task.Targets[index].Status = "succeeded"
			task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
			hasSuccessfulTarget = true
		}
		if !s.checkpointTask(&task, hasDurableMutation, "target-terminal") {
			return
		}
	}
	// A mixed batch may have already promoted verified locales into the public
	// content release. Rebuild once for those successful targets even when a
	// sibling target failed or needs review.
	if hasSuccessfulTarget && s.rebuilder != nil {
		if s.root.Err() != nil {
			return
		}
		if !s.generationCurrent(runGeneration) {
			s.finishTask(&task, "needs-review", "source-changed-before-start")
			return
		}
		if !s.checkpointResult(&task, s.updateRebuildProgress(&task, false), hasDurableMutation, "rebuild-start") {
			return
		}
		buildContext := publisher.WithBuildRequest(s.root, publisher.BuildRequest{
			Operation: "translation-rebuild", SubjectKind: task.EntityKind, SubjectID: task.EntityID, ParentTaskID: task.ID,
		})
		var buildErr error
		s.withPauseGate(func() {
			if !s.generationCurrent(runGeneration) {
				buildErr = content.ErrSourceChanged
				return
			}
			_, buildErr = s.rebuilder.Build(buildContext)
		})
		if s.root.Err() != nil {
			return
		}
		if errors.Is(buildErr, content.ErrSourceChanged) {
			s.finishTask(&task, "needs-review", "source-changed-before-start")
			return
		}
		if buildErr != nil {
			s.finishTask(&task, "failed", "rebuild-failed")
			return
		}
		if !s.checkpointResult(&task, s.updateRebuildProgress(&task, true), hasDurableMutation, "rebuild-complete") {
			return
		}
	}
	if needsReview {
		s.finishTask(&task, "needs-review", "target-needs-review")
		return
	}
	if failed {
		s.finishTask(&task, "failed", "target-failed")
		return
	}
	s.finishTask(&task, "succeeded", "")
}

// Pause waits for in-flight content commits/static rebuilds and prevents
// provider work from entering either durable phase until resume is called.
// Provider requests already in flight may finish, but their optimistic source
// and target revisions are revalidated when they later enter the commit gate.
func (s *Service) Pause() func() {
	s.runGate.Lock()
	s.generation.Add(1)
	return s.runGate.Unlock
}

func (s *Service) generationCurrent(generation uint64) bool {
	return generation != 0 && generation == s.generation.Load()
}

func (s *Service) getContent(kind, id string) (domain.Post, error) {
	switch kind {
	case "Post":
		return s.content.GetPost(id)
	case "Page":
		return s.content.GetPage(id)
	default:
		return domain.Post{}, content.ErrNotFound
	}
}

func (s *Service) applyAITranslation(kind, id, locale string, input content.ApplyAITranslationInput) (domain.Post, error) {
	switch kind {
	case "Post":
		return s.content.ApplyAITranslation(id, locale, input)
	case "Page":
		return s.content.ApplyAIPageTranslation(id, locale, input)
	default:
		return domain.Post{}, content.ErrNotFound
	}
}

func (s *Service) checkpointTask(task *Task, durableMutation bool, checkpoint string) bool {
	return s.checkpointResult(task, s.writeTask(*task), durableMutation, checkpoint)
}

func (s *Service) checkpointResult(task *Task, err error, durableMutation bool, checkpoint string) bool {
	if err == nil {
		return true
	}
	if durableMutation {
		slog.Error("persist translation checkpoint failed after durable content mutation; continuing task", "task", task.ID, "checkpoint", checkpoint, "error", err)
		return true
	}
	_ = s.finishTask(task, "failed", "progress-write-failed")
	return false
}

func (s *Service) finishTask(task *Task, status, message string) error {
	completed := time.Now().UTC()
	task.Status = status
	task.Error = message
	task.CompletedAt = &completed
	if status == "succeeded" {
		task.Progress = taskstore.Complete(task.Progress, "translation-complete")
	} else {
		task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, task.Progress.Current, task.Progress.Total, task.Progress.Percent, message)
	}
	err := s.writeTask(*task)
	if err != nil {
		slog.Error("persist terminal translation task failed", "task", task.ID, "status", status, "error", err)
		s.queueTerminalRetry(*task)
	}
	return err
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
			slog.Error("retry terminal translation task persistence failed", "task", taskID, "error", err)
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
			slog.Warn("persist translation task failed; retrying", "task", task.ID, "attempt", attempt, "error", lastErr)
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

func validTaskID(id string) bool {
	return strings.HasPrefix(id, "translation-") && len(id) < 128 && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

func validStoredTask(task Task, expectedID string) bool {
	return task.SchemaVersion == domain.SchemaVersion && task.Kind == "Translation" && task.ID == expectedID && validTaskID(task.ID)
}

func normalizeLegacyTranslationTask(task *Task) {
	targetTotal := 0
	targetCurrent := 0
	for index := range task.Targets {
		if task.Targets[index].Progress.Total < 1 {
			task.Targets[index].Progress.Total = 1
		}
		if task.Targets[index].Progress.Phase == "" {
			task.Targets[index].Progress.Phase = task.Targets[index].Status
		}
		if task.Targets[index].Status == "succeeded" && task.Targets[index].Progress.Percent < 100 {
			task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
		}
		targetTotal += task.Targets[index].Progress.Total
		targetCurrent += task.Targets[index].Progress.Current
	}
	if task.Progress.Total < 1 {
		task.Progress.Total = targetTotal + 2
		task.Progress.Current = targetCurrent
	}
	if task.Progress.Phase == "" {
		task.Progress.Phase = task.Status
	}
	if task.Status == "succeeded" && task.Progress.Percent < 100 {
		task.Progress = taskstore.Complete(task.Progress, "translation-complete")
	}
}

func toTargetTasks(locales []string, states ...map[string]domain.LocaleContentState) []TargetTask {
	targets := make([]TargetTask, 0, len(locales))
	for _, locale := range locales {
		expectedRevision := 0
		if len(states) > 0 {
			expectedRevision = states[0][locale].Revision
		}
		targets = append(targets, TargetTask{Locale: locale, ExpectedRevision: expectedRevision, Status: "queued", Progress: taskstore.Progress{Phase: "queued"}})
	}
	return targets
}

func initializeTranslationIdentity(task *Task, post domain.Post) {
	source, sourceExists := post.Content[task.SourceLocale]
	task.SourceContent = localizedContentFingerprint(source, sourceExists)
	for index := range task.Targets {
		localized, exists := post.Content[task.Targets[index].Locale]
		task.Targets[index].ExpectedContent = localizedContentFingerprint(localized, exists)
	}
}

func taskHasContentIdentity(task Task) bool {
	if task.SourceContent == "" {
		return false
	}
	for _, target := range task.Targets {
		if target.ExpectedContent == "" {
			return false
		}
	}
	return true
}

func taskMatchesSource(task Task, post domain.Post) bool {
	localized, exists := post.Content[task.SourceLocale]
	return task.SourceContent != "" && task.SourceContent == localizedContentFingerprint(localized, exists)
}

func targetMatchesExpected(target TargetTask, localized domain.LocalizedMarkdown, exists bool) bool {
	return target.ExpectedContent != "" && target.ExpectedContent == localizedContentFingerprint(localized, exists)
}

func targetMatchesResult(target TargetTask, localized domain.LocalizedMarkdown, exists bool) bool {
	return target.ResultContent != "" && target.ResultContent == localizedContentFingerprint(localized, exists)
}

func normalizeTranslationResult(localized domain.LocalizedMarkdown) domain.LocalizedMarkdown {
	localized.Title = strings.TrimSpace(localized.Title)
	localized.Summary = strings.TrimSpace(localized.Summary)
	localized.SEOTitle = strings.TrimSpace(localized.SEOTitle)
	localized.SEODescription = strings.TrimSpace(localized.SEODescription)
	localized.Markdown = strings.TrimLeft(localized.Markdown, "\r\n")
	return localized
}

func localizedContentFingerprint(localized domain.LocalizedMarkdown, exists bool) string {
	payload, _ := json.Marshal(struct {
		Exists  bool                     `json:"exists"`
		Content domain.LocalizedMarkdown `json:"content"`
	}{Exists: exists, Content: localized})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func initializeTranslationProgress(task *Task, source domain.LocalizedMarkdown, providerMaxOutputTokens ...int) {
	workUnits := translationWorkUnits(source, providerMaxOutputTokens...)
	targetTotal := 0
	targetCurrent := 0
	for index := range task.Targets {
		if task.Targets[index].Progress.Total < 1 {
			task.Targets[index].Progress.Total = workUnits
		}
		if task.Targets[index].Progress.Phase == "" {
			task.Targets[index].Progress.Phase = "queued"
		}
		targetTotal += task.Targets[index].Progress.Total
		targetCurrent += task.Targets[index].Progress.Current
	}
	// Two task-level units remain after target translation: one rebuild and one
	// final durable commit. Percent is phase-weighted separately so Current/Total
	// stays an honest work-unit counter.
	task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, targetCurrent, targetTotal+2, task.Progress.Percent, task.Progress.Message)
}

func translationWorkUnits(source domain.LocalizedMarkdown, providerMaxOutputTokens ...int) int {
	units := 2 // metadata and durable save/promotion
	if strings.TrimSpace(source.Markdown) != "" {
		chunkRunes := translationChunkRunes
		if len(providerMaxOutputTokens) > 0 {
			chunkRunes = translationChunkRuneLimit(providerMaxOutputTokens[0])
		}
		units += len(segmentMarkdownParts(source.Markdown, chunkRunes))
	}
	return units
}

func (s *Service) updateTargetProgress(task *Task, index int, phase string, current, total int, message string) error {
	if index < 0 || index >= len(task.Targets) {
		return ErrTaskNotFound
	}
	target := &task.Targets[index]
	target.Progress = taskstore.Advance(target.Progress, phase, current, total, -1, message)
	targetCurrent := 0
	targetTotal := 0
	for targetIndex := range task.Targets {
		targetCurrent += task.Targets[targetIndex].Progress.Current
		targetTotal += task.Targets[targetIndex].Progress.Total
	}
	percent := 0
	if targetTotal > 0 {
		percent = targetCurrent * 90 / targetTotal
	}
	task.Progress = taskstore.Advance(task.Progress, "translation-"+phase, targetCurrent, targetTotal+2, percent, message)
	return s.writeTask(*task)
}

func (s *Service) updateRebuildProgress(task *Task, completed bool) error {
	targetTotal := 0
	for index := range task.Targets {
		targetTotal += task.Targets[index].Progress.Total
	}
	current := targetTotal
	percent := 94
	message := "rebuilding-static-site"
	if completed {
		current++
		percent = 98
		message = "static-rebuild-complete"
	}
	task.Progress = taskstore.Advance(task.Progress, "translation-rebuild", current, targetTotal+2, percent, message)
	return s.writeTask(*task)
}

func taskStatusForError(err error) string {
	if errors.Is(err, content.ErrSourceChanged) || errors.Is(err, content.ErrTargetChanged) || errors.Is(err, content.ErrManualProtected) {
		return "needs-review"
	}
	return "failed"
}

func safeTaskError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "translation-timeout"
	case errors.Is(err, content.ErrSourceChanged):
		return "source-changed"
	case errors.Is(err, content.ErrTargetChanged):
		return "target-changed"
	case errors.Is(err, content.ErrManualProtected):
		return "manual-protected"
	case errors.Is(err, ai.ErrKeyMissing):
		return "provider-key-missing"
	case errors.Is(err, ai.ErrProviderFailed):
		return "provider-request-failed"
	case errors.Is(err, ai.ErrInvalidProvider), errors.Is(err, ai.ErrProviderNotFound):
		return "provider-unavailable"
	case errors.Is(err, ErrUnsafeOutput):
		return "unsafe-output"
	default:
		return "translation-failed"
	}
}

func newTaskID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "translation-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

func randomTokenPrefix() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}
