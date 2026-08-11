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
	ErrNoTargets                        = errors.New("no translation targets")
	ErrManualConfirmation               = errors.New("manual translations require confirmation")
	ErrTaskNotFound                     = errors.New("translation task not found")
	ErrPublicationGenerationUnavailable = errors.New("publication generation is unavailable")
)

const (
	translationTargetTimeout    = 15 * time.Minute
	taskCheckpointWriteAttempts = 3
	promotionRetryAttempts      = 3
	promotionRetryBaseDelay     = 100 * time.Millisecond
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

type buildTaskReader interface {
	GetTask(string) (publisher.Task, error)
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
	SchemaVersion int    `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string `yaml:"id" json:"id"`
	Kind          string `yaml:"kind" json:"kind"`
	EntityKind    string `yaml:"entityKind" json:"entityKind"`
	EntityID      string `yaml:"entityId" json:"entityId"`
	// PublicationRevision records the public release that requested automatic
	// translation. The task remains bound to the immutable published source
	// identity, not to the mutable editing head; later releases may reuse the
	// same work only while that exact source identity is still public.
	PublicationRevision int `yaml:"publicationRevision,omitempty" json:"publicationRevision,omitempty"`
	// PublicationGeneration is the stable explicit-publish identity. Release
	// revisions advance for each promoted target, so this value distinguishes a
	// task's own incremental release writes from a newer same-source publish.
	// Zero is reserved for legacy task records, which retain the older
	// source-identity-only recovery behavior.
	PublicationGeneration int                `yaml:"publicationGeneration,omitempty" json:"publicationGeneration,omitempty"`
	SourceLocale          string             `yaml:"sourceLocale" json:"sourceLocale"`
	SourceRevision        int                `yaml:"sourceRevision" json:"sourceRevision"`
	ProviderID            string             `yaml:"providerId" json:"providerId"`
	Model                 string             `yaml:"model" json:"model"`
	OverwriteManual       bool               `yaml:"overwriteManual" json:"overwriteManual"`
	SourceContent         string             `yaml:"sourceContent,omitempty" json:"sourceContent,omitempty"`
	Status                string             `yaml:"status" json:"status"`
	Progress              taskstore.Progress `yaml:"progress" json:"progress"`
	Targets               []TargetTask       `yaml:"targets" json:"targets"`
	BuildStatus           string             `yaml:"buildStatus,omitempty" json:"buildStatus,omitempty"`
	BuildTaskID           string             `yaml:"buildTaskId,omitempty" json:"buildTaskId,omitempty"`
	CreatedAt             time.Time          `yaml:"createdAt" json:"createdAt"`
	StartedAt             *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt           *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Error                 string             `yaml:"error,omitempty" json:"error,omitempty"`
}

type StartInput struct {
	EntityKind      string
	PostID          string
	Locales         []string
	OverwriteManual bool
	SkipManual      bool
	// PublishedRelease binds the task to the current immutable public release.
	// Automatic publish flows use this so a later draft save cannot invalidate
	// translation of the version visitors are waiting to receive.
	PublishedRelease bool
	// PublicationGeneration optionally requires one exact public release. A
	// scheduled parent supplies its pre-reserved value so a persisted child
	// receipt cannot attach to a later same-source publication.
	PublicationGeneration int
	// SkipCurrentAI omits targets already generated from this exact source
	// revision. It also enables compatible in-flight task reuse for idempotent
	// repeated publications.
	SkipCurrentAI bool
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
	// promoteAIHook is a narrow test seam for a transient failure after the AI
	// head write and before public promotion. Production uses content directly.
	promoteAIHook func(string, string, string, int) error
	// afterReleaseApplyRead is a narrow test seam for the notification-only
	// reread after a release-only commit. Production always uses getContent.
	afterReleaseApplyRead func(string, string) (domain.Post, error)
	retryWait             func(context.Context, time.Duration) error
	root                  context.Context
	cancel                context.CancelFunc
	lifecycleMu           sync.Mutex
	closed                bool
	launched              map[string]struct{}
	runWG                 sync.WaitGroup
	terminalMu            sync.Mutex
	terminalClosed        bool
	terminalRetries       map[string]terminalRetry
	terminalWG            sync.WaitGroup
	generation            atomic.Uint64
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
		if s.publicationGenerationUnavailable(task) {
			if err := s.finishTask(&task, "needs-review", "publication-generation-unavailable"); err != nil {
				slog.Error("persist legacy publication task terminal failed; background retry queued", "task", task.ID, "error", err)
			}
			recovered++
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
	publicationRevision := 0
	publicationGeneration := 0
	if input.PublishedRelease {
		post, err = s.content.GetPublishedRelease(entityKind, input.PostID)
		if err != nil {
			return Task{}, false, err
		}
		publicationRevision = post.Meta.ReleaseRevision
		if publicationRevision < 1 {
			publicationRevision = post.Meta.Revision
		}
		publicationGeneration = post.Meta.PublicationGeneration
		if publicationGeneration <= 0 {
			// Automatic publication must have a stable release identity. Startup
			// migration supplies it for legacy repositories; accepting a zero here
			// would let an old same-source task cross an explicit publish boundary.
			return Task{}, false, ErrPublicationGenerationUnavailable
		}
		if input.PublicationGeneration > 0 && publicationGeneration != input.PublicationGeneration {
			return Task{}, false, content.ErrSourceChanged
		}
	}
	targets, manual, err := s.targets(post, input.Locales, input.PublishedRelease)
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
	if input.SkipCurrentAI {
		requestedTargets := append([]string(nil), targets...)
		targets = staleAITargets(post, targets)
		if existing, ok, activeErr := s.compatibleActiveTask(post, publicationRevision > 0, publicationGeneration, input.OverwriteManual, requestedTargets, targets); activeErr != nil {
			return Task{}, false, activeErr
		} else if ok {
			return existing, false, nil
		}
		if len(targets) == 0 {
			return Task{}, false, ErrNoTargets
		}
	}
	provider, _, err := s.ai.DefaultCredentials()
	if err != nil {
		if input.RecordPreflightFailure && recordableProviderPreflightError(err) {
			if existing, ok, listErr := s.matchingPreflightFailure(post, publicationRevision > 0, publicationGeneration, input.OverwriteManual, targets, safeTaskError(err)); listErr != nil {
				return Task{}, false, errors.Join(err, fmt.Errorf("find translation preflight failure: %w", listErr))
			} else if ok {
				return existing, false, err
			}
			task, recordErr := s.recordPreflightFailure(post, entityKind, publicationRevision, publicationGeneration, targets, input.OverwriteManual, err)
			if recordErr != nil {
				return Task{}, false, errors.Join(err, fmt.Errorf("persist translation preflight failure: %w", recordErr))
			}
			return task, false, err
		}
		return Task{}, false, err
	}
	if existing, ok, err := s.activeTask(post, publicationRevision > 0, publicationGeneration, provider.ID, input.OverwriteManual, targets); err != nil {
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
		PublicationRevision:   publicationRevision,
		PublicationGeneration: publicationGeneration,
		SourceLocale:          post.Meta.SourceLocale, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
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

func (s *Service) recordPreflightFailure(post domain.Post, entityKind string, publicationRevision, publicationGeneration int, targets []string, overwriteManual bool, cause error) (Task, error) {
	taskID, err := newTaskID()
	if err != nil {
		return Task{}, err
	}
	now := time.Now().UTC()
	safeError := safeTaskError(cause)
	task := Task{
		SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "Translation", EntityKind: entityKind, EntityID: post.Meta.ID,
		PublicationRevision:   publicationRevision,
		PublicationGeneration: publicationGeneration,
		SourceLocale:          post.Meta.SourceLocale, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
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
func (s *Service) activeTask(post domain.Post, publication bool, publicationGeneration int, providerID string, overwriteManual bool, targets []string) (Task, bool, error) {
	tasks, err := s.List()
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if task.EntityKind != post.Meta.Kind || task.EntityID != post.Meta.ID || !taskMatchesPublicationGeneration(task, publication, publicationGeneration) || task.SourceLocale != post.Meta.SourceLocale || task.SourceRevision != post.Meta.Locales[post.Meta.SourceLocale].Revision || task.ProviderID != providerID || task.OverwriteManual != overwriteManual {
			continue
		}
		if sameTargets(task.Targets, targets) && taskMatchesSource(task, post) && taskMatchesCurrentTargets(task, post) {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

// compatibleActiveTask handles the automatic-publication subset case. A task
// may already have completed one target while another publication request is
// prepared; that completed target is now filtered as current, but the original
// in-flight task still owns all remaining stale targets. Reusing that superset
// avoids a second provider call and a second final rebuild.
func (s *Service) compatibleActiveTask(post domain.Post, publication bool, publicationGeneration int, overwriteManual bool, requested, stale []string) (Task, bool, error) {
	tasks, err := s.List()
	if err != nil {
		return Task{}, false, err
	}
	requestedSet := stringSet(requested)
	staleSet := stringSet(stale)
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if task.EntityKind != post.Meta.Kind || task.EntityID != post.Meta.ID || !taskMatchesPublicationGeneration(task, publication, publicationGeneration) || task.SourceLocale != post.Meta.SourceLocale || task.SourceRevision != post.Meta.Locales[post.Meta.SourceLocale].Revision || task.OverwriteManual != overwriteManual || !taskMatchesSource(task, post) {
			continue
		}
		covered := make(map[string]bool, len(task.Targets))
		valid := true
		for _, target := range task.Targets {
			if !requestedSet[target.Locale] {
				valid = false
				break
			}
			covered[target.Locale] = true
		}
		if !valid {
			continue
		}
		for locale := range staleSet {
			if !covered[locale] {
				valid = false
				break
			}
		}
		if valid && taskMatchesCurrentTargets(task, post) {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func (s *Service) matchingPreflightFailure(post domain.Post, publication bool, publicationGeneration int, overwriteManual bool, targets []string, errorCode string) (Task, bool, error) {
	tasks, err := s.List()
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range tasks {
		if task.Status != "failed" || task.Error != errorCode {
			continue
		}
		if task.EntityKind != post.Meta.Kind || task.EntityID != post.Meta.ID || !taskMatchesPublicationGeneration(task, publication, publicationGeneration) || task.SourceLocale != post.Meta.SourceLocale || task.SourceRevision != post.Meta.Locales[post.Meta.SourceLocale].Revision || task.OverwriteManual != overwriteManual {
			continue
		}
		if sameTargets(task.Targets, targets) && taskMatchesSource(task, post) && taskMatchesCurrentTargets(task, post) {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func taskMatchesPublicationGeneration(task Task, publication bool, generation int) bool {
	if !publication {
		return task.PublicationRevision == 0 && task.PublicationGeneration == 0
	}
	// A missing generation is an old persisted task record. Do not allow it to
	// acquire a new explicit publication merely because its source revision and
	// content happen to collide after a restart or restore.
	return task.PublicationRevision > 0 && generation > 0 && task.PublicationGeneration == generation
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

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
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

func (s *Service) targets(post domain.Post, requested []string, readyOnly bool) ([]string, []string, error) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return nil, nil, err
	}
	enabled := make(map[string]bool)
	for _, locale := range config.Enabled {
		ready := locale.Status == "" || locale.Status == domain.LocaleStatusReady
		if locale.Enabled && locale.Code != post.Meta.SourceLocale && (!readyOnly || ready) {
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

func staleAITargets(post domain.Post, targets []string) []string {
	stale := make([]string, 0, len(targets))
	sourceRevision := post.Meta.Locales[post.Meta.SourceLocale].Revision
	source := post.Content[post.Meta.SourceLocale]
	for _, locale := range targets {
		state, stateExists := post.Meta.Locales[locale]
		target, contentExists := post.Content[locale]
		if stateExists && contentExists && state.Origin == domain.LocaleOriginAI && state.State == "current" && state.SourceRevision == sourceRevision && completeLocalizedMarkdown(source, target) {
			continue
		}
		stale = append(stale, locale)
	}
	return stale
}

func completeLocalizedMarkdown(source, target domain.LocalizedMarkdown) bool {
	if strings.TrimSpace(target.Title) == "" {
		return false
	}
	for _, pair := range [][2]string{
		{source.Summary, target.Summary},
		{source.SEOTitle, target.SEOTitle},
		{source.SEODescription, target.SEODescription},
		{source.Markdown, target.Markdown},
	} {
		if strings.TrimSpace(pair[0]) != "" && strings.TrimSpace(pair[1]) == "" {
			return false
		}
	}
	return true
}

func allTargetsSucceeded(task Task) bool {
	if len(task.Targets) == 0 {
		return false
	}
	for _, target := range task.Targets {
		if target.Status != "succeeded" {
			return false
		}
	}
	return true
}

// publicationTargetsCurrent proves that the immutable release about to be
// rendered contains every result owned by this task. A task checkpoint alone
// is insufficient: a later explicit publish deliberately marks all targets
// stale while retaining their files, and that release must never be built by
// the older task.
func publicationTargetsCurrent(task Task, published domain.Post) bool {
	if !taskMatchesPublication(task, published) {
		return false
	}
	source := published.Content[published.Meta.SourceLocale]
	for _, target := range task.Targets {
		state, exists := published.Meta.Locales[target.Locale]
		localized, contentExists := published.Content[target.Locale]
		if !exists || !contentExists || state.Origin != domain.LocaleOriginAI || state.State != "current" || state.SourceRevision != task.SourceRevision || !completeLocalizedMarkdown(source, localized) || !targetMatchesResult(target, localized, contentExists) {
			return false
		}
	}
	return true
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
	if s.publicationGenerationUnavailable(task) {
		s.finishTask(&task, "needs-review", "publication-generation-unavailable")
		return
	}
	runGeneration := s.generation.Load()
	post, err := s.taskContent(task)
	if err != nil {
		s.finishTask(&task, "needs-review", "source-changed-before-start")
		return
	}
	if !taskMatchesSource(task, post) {
		s.finishTask(&task, "needs-review", "source-changed-before-start")
		return
	}
	hasDurableMutation := s.taskHasDurableMutation(task, post)
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
		post, postErr := s.taskContent(task)
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
		currentAI := targetStateExists && targetState.Origin == domain.LocaleOriginAI && targetState.State == "current" && targetState.SourceRevision == task.SourceRevision
		completeCurrentAI := currentAI && targetExists && completeLocalizedMarkdown(post.Content[post.Meta.SourceLocale], targetContent)
		if task.PublicationRevision > 0 && completeCurrentAI {
			// Overlapping publication tasks can legitimately observe a target that
			// a sibling task completed against the exact same public source. Trust
			// that durable AI result and adopt its identity so target-set expansion
			// converges on one coherent final build instead of dead-ending on the
			// older ExpectedContent fingerprint.
			task.Targets[index].ResultContent = localizedContentFingerprint(targetContent, true)
			completed := time.Now().UTC()
			task.Targets[index].Status = "succeeded"
			task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
			task.Targets[index].Error = ""
			task.Targets[index].CompletedAt = &completed
			hasSuccessfulTarget = true
			hasDurableMutation = true
			if !s.checkpointTask(&task, hasDurableMutation, "observed-current-publication-target") {
				return
			}
			continue
		}
		if task.Targets[index].Status == "succeeded" {
			if !currentAI || !resultMatches || (task.PublicationRevision > 0 && !completeCurrentAI) {
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
		if currentAI {
			if !resultMatches || (task.PublicationRevision > 0 && !completeCurrentAI) {
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
			if task.PublicationRevision > 0 {
				completed := time.Now().UTC()
				task.Targets[index].Status = "succeeded"
				task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
				hasSuccessfulTarget = true
				task.Targets[index].Error = ""
				task.Targets[index].CompletedAt = &completed
				if !s.checkpointTask(&task, hasDurableMutation, "recovered-published-target-complete") {
					return
				}
				continue
			}
			var promoteErr error
			notifyChange := false
			s.withDurableContentMutation(func() {
				if !s.generationCurrent(runGeneration) {
					promoteErr = content.ErrSourceChanged
					return
				}
				promoteErr = s.promoteAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, task.SourceRevision)
				// The AI head mutation is already durable in this recovery branch even
				// if public promotion now fails, so its old schedule must still retire.
				notifyChange = true
			}, func() {
				if notifyChange {
					s.notifyContentChanged(task.EntityKind, task.EntityID, post.Meta.Revision)
				}
			})
			if promoteErr != nil {
				if recoverablePromotionError(promoteErr) {
					slog.Error("promote AI translation failed after durable head write; leaving task recoverable", "task", task.ID, "target", task.Targets[index].Locale, "error", promoteErr)
					return
				}
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
		if task.PublicationRevision > 0 {
			recovered, recoverErr := s.recoverPublishedHeadResult(&task, index, runGeneration)
			if recoverErr != nil {
				if recoverablePromotionError(recoverErr) {
					slog.Error("recover public AI promotion failed after durable head write; leaving task recoverable", "task", task.ID, "target", task.Targets[index].Locale, "error", recoverErr)
					return
				}
				failed = true
				needsReview = needsReview || errors.Is(recoverErr, content.ErrSourceChanged) || errors.Is(recoverErr, content.ErrTargetChanged)
				completed := time.Now().UTC()
				task.Targets[index].Status = taskStatusForError(recoverErr)
				task.Targets[index].Error = safeTaskError(recoverErr)
				task.Targets[index].CompletedAt = &completed
				if !s.checkpointTask(&task, hasDurableMutation, "published-head-result-recovery-failed") {
					return
				}
				continue
			}
			if recovered {
				hasDurableMutation = true
				hasSuccessfulTarget = true
				completed := time.Now().UTC()
				task.Targets[index].Status = "succeeded"
				task.Targets[index].Progress = taskstore.Complete(task.Targets[index].Progress, "target-complete")
				task.Targets[index].Error = ""
				task.Targets[index].CompletedAt = &completed
				if !s.checkpointTask(&task, hasDurableMutation, "published-head-result-recovered") {
					return
				}
				continue
			}
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
			// ResultContent is the recovery proof for an Apply-before-Promote
			// crash. It is an unconditional write-ahead barrier: a durable sibling
			// must never downgrade failure here into permission to mutate this target.
			if !s.checkpointMutationBarrier(&task, s.updateTargetProgress(&task, index, "save", total-1, total, "saving-translation"), hasDurableMutation, "target-ready-to-save") {
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
				latest, latestErr := s.taskContent(task)
				if latestErr != nil || !taskMatchesSource(task, latest) || latest.Meta.Locales[latest.Meta.SourceLocale].Revision != task.SourceRevision {
					translateErr = content.ErrSourceChanged
					return
				}
				latestTarget, latestTargetExists := latest.Content[task.Targets[index].Locale]
				latestTargetRevision := latest.Meta.Locales[task.Targets[index].Locale].Revision
				if latestTargetRevision != expectedTargetRevision || !targetMatchesExpected(task.Targets[index], latestTarget, latestTargetExists) {
					translateErr = content.ErrTargetChanged
					return
				}
				if task.PublicationRevision > 0 {
					updated, applied, translateErr = s.applyPublishedTranslation(task, task.Targets[index], latest, translated)
				} else {
					updated, translateErr = s.applyAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, content.ApplyAITranslationInput{
						ExpectedSourceRevision: task.SourceRevision, ExpectedTargetRevision: &expectedTargetRevision, OverwriteManual: task.OverwriteManual, Content: translated,
					})
					applied = translateErr == nil
					if applied {
						translateErr = s.promoteAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, task.SourceRevision)
					}
				}
			}, func() {
				if applied && updated.Meta.ID != "" {
					s.notifyContentChanged(task.EntityKind, task.EntityID, updated.Meta.Revision)
				}
			})
			if applied {
				hasDurableMutation = true
				if translateErr != nil && recoverablePromotionError(translateErr) {
					slog.Error("promote AI translation failed after durable head write; leaving task recoverable", "task", task.ID, "target", task.Targets[index].Locale, "error", translateErr)
					return
				}
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
	// Legacy/manual work may publish the successful portion of a mixed batch.
	// Automatic publication work is fail-closed: every selected target must be
	// coherent before its one final build can replace the old generated site.
	buildSuccessfulTargets := hasSuccessfulTarget && (task.PublicationRevision == 0 || (!failed && !needsReview && allTargetsSucceeded(task)))
	if buildSuccessfulTargets && s.rebuilder != nil {
		if s.root.Err() != nil {
			return
		}
		if !s.generationCurrent(runGeneration) {
			s.finishTask(&task, "needs-review", "source-changed-before-start")
			return
		}
		if task.BuildStatus == "succeeded" {
			if !s.checkpointResult(&task, s.updateRebuildProgress(&task, true), hasDurableMutation, "recovered-rebuild-complete") {
				return
			}
		} else if task.BuildTaskID != "" {
			if reader, ok := s.rebuilder.(buildTaskReader); ok {
				if buildTask, readErr := reader.GetTask(task.BuildTaskID); readErr == nil && buildTask.Status == "succeeded" {
					task.BuildStatus = "succeeded"
					if !s.checkpointResult(&task, s.updateRebuildProgress(&task, true), hasDurableMutation, "observed-rebuild-complete") {
						return
					}
				}
			}
		}
		if task.BuildStatus != "succeeded" {
			buildTaskID, buildIDErr := publisher.NewBuildTaskID()
			if buildIDErr != nil {
				s.finishTask(&task, "failed", "rebuild-task-unavailable")
				return
			}
			task.BuildTaskID = buildTaskID
			task.BuildStatus = "queued"
			if !s.checkpointResult(&task, s.updateRebuildProgress(&task, false), hasDurableMutation, "rebuild-queued") {
				return
			}
			buildContext := publisher.WithBuildRequest(s.root, publisher.BuildRequest{
				TaskID: task.BuildTaskID, Operation: "translation-rebuild", SubjectKind: task.EntityKind, SubjectID: task.EntityID, ParentTaskID: task.ID,
			})
			var buildErr error
			task.BuildStatus = "running"
			if !s.checkpointTask(&task, hasDurableMutation, "rebuild-start") {
				return
			}
			buildWork := func() {
				if !s.generationCurrent(runGeneration) {
					buildErr = content.ErrSourceChanged
					return
				}
				latest, latestErr := s.taskContent(task)
				if latestErr != nil || !taskMatchesSource(task, latest) || latest.Meta.Locales[latest.Meta.SourceLocale].Revision != task.SourceRevision {
					buildErr = content.ErrSourceChanged
					return
				}
				if task.PublicationRevision > 0 && !publicationTargetsCurrent(task, latest) {
					buildErr = content.ErrTargetChanged
					return
				}
				_, buildErr = s.rebuilder.Build(buildContext)
			}
			// Rendering observes immutable public releases. Keep its existing
			// short pause fence rather than holding the server-wide durable
			// mutation lock for an entire render; generation/CAS checks already
			// prevent this task from mutating a later explicit publication.
			s.withPauseGate(buildWork)
			if s.root.Err() != nil {
				return
			}
			if errors.Is(buildErr, content.ErrSourceChanged) || errors.Is(buildErr, content.ErrTargetChanged) {
				s.finishTask(&task, "needs-review", "source-changed-before-start")
				return
			}
			if buildErr != nil {
				task.BuildStatus = "failed"
				s.finishTask(&task, "failed", "rebuild-failed")
				return
			}
			task.BuildStatus = "succeeded"
			if !s.checkpointResult(&task, s.updateRebuildProgress(&task, true), hasDurableMutation, "rebuild-complete") {
				return
			}
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

// taskHasDurableMutation also checks the editable head for publication tasks.
// A crash can happen after ApplyAITranslation durably writes the head but
// before PromoteAITranslation updates the public release. In that window the
// release alone looks untouched; treating the task as pristine would let a
// transient start-checkpoint failure abort before recoverPublishedHeadResult
// can repair the promotion.
func (s *Service) taskHasDurableMutation(task Task, taskPost domain.Post) bool {
	if task.PublicationRevision > 0 && !taskMatchesPublication(task, taskPost) {
		return false
	}
	for _, target := range task.Targets {
		state, exists := taskPost.Meta.Locales[target.Locale]
		if target.Status == "succeeded" || (exists && state.Origin == domain.LocaleOriginAI && state.State == "current" && state.SourceRevision == task.SourceRevision) {
			return true
		}
	}
	if task.PublicationRevision <= 0 {
		return false
	}
	head, err := s.getContent(task.EntityKind, task.EntityID)
	if err != nil || !taskMatchesPublication(task, head) {
		return false
	}
	for _, target := range task.Targets {
		state, exists := head.Meta.Locales[target.Locale]
		localized, contentExists := head.Content[target.Locale]
		if target.ResultContent != "" && exists && state.Origin == domain.LocaleOriginAI && state.State == "current" && state.SourceRevision == task.SourceRevision && targetMatchesResult(target, localized, contentExists) {
			return true
		}
	}
	return false
}

func (s *Service) taskContent(task Task) (domain.Post, error) {
	if task.PublicationRevision > 0 {
		published, err := s.content.GetPublishedRelease(task.EntityKind, task.EntityID)
		if err != nil {
			return domain.Post{}, err
		}
		if !taskMatchesPublication(task, published) {
			return domain.Post{}, content.ErrSourceChanged
		}
		return published, nil
	}
	return s.getContent(task.EntityKind, task.EntityID)
}

func (s *Service) publicationGenerationUnavailable(task Task) bool {
	if task.PublicationRevision <= 0 || task.PublicationGeneration > 0 {
		return false
	}
	published, err := s.content.GetPublishedRelease(task.EntityKind, task.EntityID)
	return err == nil && published.Meta.PublicationGeneration > 0
}

// taskMatchesPublication verifies stable explicit-publish ownership. A legacy
// task record without the new generation is never allowed to mutate a release
// once that release has been migrated or newly published with the marker.
func taskMatchesPublication(task Task, published domain.Post) bool {
	if task.PublicationRevision <= 0 {
		return true
	}
	if task.PublicationGeneration <= 0 {
		return published.Meta.PublicationGeneration <= 0
	}
	return published.Meta.PublicationGeneration == task.PublicationGeneration
}

// applyPublishedTranslation commits a result to the current public release
// only while its source identity still matches the publication task. When the
// editing head still shares that source, the ordinary Apply + Promote path
// keeps head and release coherent. A later source-only save makes the head a
// separate draft, so only the immutable release is updated.
func (s *Service) applyPublishedTranslation(task Task, target TargetTask, released domain.Post, translated domain.LocalizedMarkdown) (domain.Post, bool, error) {
	if !taskMatchesPublication(task, released) || !taskMatchesSource(task, released) || released.Meta.Locales[released.Meta.SourceLocale].Revision != task.SourceRevision {
		return domain.Post{}, false, content.ErrSourceChanged
	}
	head, err := s.getContent(task.EntityKind, task.EntityID)
	if err != nil {
		return domain.Post{}, false, err
	}
	if !taskMatchesPublication(task, head) {
		return domain.Post{}, false, content.ErrSourceChanged
	}
	if taskMatchesSource(task, head) && head.Meta.Locales[head.Meta.SourceLocale].Revision == task.SourceRevision {
		headTarget, headTargetExists := head.Content[target.Locale]
		if !targetMatchesExpected(target, headTarget, headTargetExists) {
			return domain.Post{}, false, content.ErrTargetChanged
		}
		expectedTargetRevision := target.ExpectedRevision
		updated, applyErr := s.applyAITranslation(task.EntityKind, task.EntityID, target.Locale, content.ApplyAITranslationInput{
			ExpectedSourceRevision: task.SourceRevision, ExpectedTargetRevision: &expectedTargetRevision, ExpectedPublicationGeneration: task.PublicationGeneration, OverwriteManual: task.OverwriteManual, Content: translated,
		})
		applied := applyErr == nil
		if applyErr == nil {
			applyErr = s.promoteAITranslation(task.EntityKind, task.EntityID, target.Locale, task.SourceRevision, task.PublicationGeneration)
		}
		return updated, applied, applyErr
	}
	applyErr := s.content.ApplyAIReleaseTranslation(task.EntityKind, task.EntityID, target.Locale, content.ApplyAIReleaseTranslationInput{
		ExpectedReleaseRevision:       released.Meta.Revision,
		ExpectedSourceRevision:        task.SourceRevision,
		ExpectedTargetRevision:        target.ExpectedRevision,
		ExpectedPublicationGeneration: task.PublicationGeneration,
		Content:                       translated,
	})
	if applyErr != nil {
		return domain.Post{}, false, applyErr
	}
	readHead := s.getContent
	if s.afterReleaseApplyRead != nil {
		readHead = s.afterReleaseApplyRead
	}
	updated, readErr := readHead(task.EntityKind, task.EntityID)
	if readErr != nil {
		// The release mutation is already durable. This reread exists only to
		// decorate the scheduler notification, so a transient failure must not turn
		// committed translation into a terminal task failure with no final build.
		slog.Warn("reread head after release translation failed; skipping content-change notification", "task", task.ID, "kind", task.EntityKind, "id", task.EntityID, "error", readErr)
		return domain.Post{}, true, nil
	}
	return updated, true, nil
}

// recoverPublishedHeadResult repairs the crash window between a durable head
// Apply and its public Promote. It deliberately ignores a newer draft source;
// that case is owned by ApplyAIReleaseTranslation after the provider result is
// recovered or regenerated against the still-current public source.
func (s *Service) recoverPublishedHeadResult(task *Task, targetIndex int, runGeneration uint64) (bool, error) {
	target := task.Targets[targetIndex]
	if target.ResultContent == "" {
		return false, nil
	}
	head, err := s.getContent(task.EntityKind, task.EntityID)
	if err != nil {
		return false, err
	}
	if !taskMatchesPublication(*task, head) || !taskMatchesSource(*task, head) || head.Meta.Locales[head.Meta.SourceLocale].Revision != task.SourceRevision {
		return false, content.ErrSourceChanged
	}
	state, exists := head.Meta.Locales[target.Locale]
	localized, contentExists := head.Content[target.Locale]
	if !exists || state.Origin != domain.LocaleOriginAI || state.State != "current" || state.SourceRevision != task.SourceRevision || !targetMatchesResult(target, localized, contentExists) {
		return false, nil
	}
	var promoteErr error
	notifyRevision := head.Meta.Revision
	s.withDurableContentMutation(func() {
		if !s.generationCurrent(runGeneration) {
			promoteErr = content.ErrSourceChanged
			return
		}
		latest, latestErr := s.taskContent(*task)
		if latestErr != nil || !taskMatchesPublication(*task, latest) || !taskMatchesSource(*task, latest) || latest.Meta.Locales[latest.Meta.SourceLocale].Revision != task.SourceRevision {
			promoteErr = content.ErrSourceChanged
			return
		}
		latestHead, headErr := s.getContent(task.EntityKind, task.EntityID)
		if headErr != nil || !taskMatchesPublication(*task, latestHead) || !taskMatchesSource(*task, latestHead) || latestHead.Meta.Locales[latestHead.Meta.SourceLocale].Revision != task.SourceRevision {
			promoteErr = content.ErrSourceChanged
			return
		}
		latestState, latestStateExists := latestHead.Meta.Locales[target.Locale]
		latestTarget, latestTargetExists := latestHead.Content[target.Locale]
		if !latestStateExists || latestState.Origin != domain.LocaleOriginAI || latestState.State != "current" || latestState.SourceRevision != task.SourceRevision || !targetMatchesResult(target, latestTarget, latestTargetExists) {
			promoteErr = content.ErrTargetChanged
			return
		}
		notifyRevision = latestHead.Meta.Revision
		promoteErr = s.promoteAITranslation(task.EntityKind, task.EntityID, target.Locale, task.SourceRevision, task.PublicationGeneration)
	}, func() {
		if promoteErr == nil {
			s.notifyContentChanged(task.EntityKind, task.EntityID, notifyRevision)
		}
	})
	return promoteErr == nil, promoteErr
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

func (s *Service) promoteAITranslation(kind, id, locale string, sourceRevision int, publicationGeneration ...int) error {
	expectedGeneration := 0
	if len(publicationGeneration) > 0 {
		expectedGeneration = publicationGeneration[0]
	}
	var promoteErr error
	for attempt := 0; attempt < promotionRetryAttempts; attempt++ {
		if s.promoteAIHook != nil {
			promoteErr = s.promoteAIHook(kind, id, locale, sourceRevision)
		} else if expectedGeneration > 0 {
			promoteErr = s.content.PromoteAITranslationForPublication(kind, id, locale, sourceRevision, expectedGeneration)
		} else {
			promoteErr = s.content.PromoteAITranslation(kind, id, locale, sourceRevision)
		}
		if promoteErr == nil || !recoverablePromotionError(promoteErr) || attempt == promotionRetryAttempts-1 {
			return promoteErr
		}
		if waitErr := s.waitProviderRetry(s.root, time.Duration(attempt+1)*promotionRetryBaseDelay); waitErr != nil {
			return waitErr
		}
	}
	return promoteErr
}

func recoverablePromotionError(err error) bool {
	if err == nil {
		return false
	}
	for _, terminal := range []error{
		content.ErrSourceChanged,
		content.ErrTargetChanged,
		content.ErrNotFound,
		content.ErrInvalidStatus,
		content.ErrLocaleDisabled,
		content.ErrManualProtected,
		content.ErrConflict,
	} {
		if errors.Is(err, terminal) {
			return false
		}
	}
	return true
}

func (s *Service) checkpointTask(task *Task, durableMutation bool, checkpoint string) bool {
	return s.checkpointResult(task, s.writeTask(*task), durableMutation, checkpoint)
}

func (s *Service) checkpointMutationBarrier(task *Task, err error, durableMutation bool, checkpoint string) bool {
	if err == nil {
		return true
	}
	if durableMutation {
		// Preserve the last running checkpoint for restart recovery. Terminalizing
		// here would strand already-committed siblings, while continuing would
		// create a mutation whose result identity was never made durable.
		slog.Error("persist translation write-ahead checkpoint failed; leaving task recoverable", "task", task.ID, "checkpoint", checkpoint, "error", err)
		return false
	}
	return s.checkpointResult(task, err, false, checkpoint)
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
	if task.SchemaVersion != domain.SchemaVersion || task.Kind != "Translation" || task.ID != expectedID || !validTaskID(task.ID) || task.PublicationRevision < 0 || task.PublicationGeneration < 0 || (task.PublicationRevision == 0 && task.PublicationGeneration != 0) {
		return false
	}
	if task.BuildTaskID != "" && !taskstore.ValidStaticBuildID(task.BuildTaskID) {
		return false
	}
	switch task.BuildStatus {
	case "":
		return true
	case "queued", "running", "succeeded", "failed":
		return task.BuildTaskID != ""
	default:
		return false
	}
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
	if task.PublicationRevision > 0 && task.PublicationGeneration == 0 {
		// Constructors in tests and trusted in-process callers may still build a
		// task literal. Persist the release marker with every newly-written task;
		// old YAML never calls this helper and remains fail-closed after migration.
		task.PublicationGeneration = post.Meta.PublicationGeneration
	}
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
