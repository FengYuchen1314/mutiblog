package localization

import (
	"context"
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
	ErrLocaleProvisionTaskNotFound  = errors.New("locale provisioning task not found")
	ErrLocaleProvisionTaskInput     = errors.New("locale provisioning task input is invalid")
	ErrLocaleProvisionTaskNotQueued = errors.New("locale provisioning task is no longer queued")
)

const (
	localeProvisionTaskKind       = "LocaleProvision"
	localeProvisionTaskOperation  = "localize-site"
	localeProvisionWriteAttempts  = 3
	localeProvisionTaskIDPrefix   = "locale-provision-"
	localeProvisionSourceLocale   = "zh-CN"
	localeProvisionTargetComplete = "locale-localization-complete"
)

// Provisioner is deliberately the narrow, no-build business primitive. The
// durable task service owns batching and invokes it once per target locale so
// a full-site locale update produces exactly one static-build child.
type Provisioner interface {
	Provision(context.Context, string) (Report, error)
}

// LocaleTaskRebuilder is the publisher boundary used by locale-provisioning
// tasks. A real publisher receives a BuildRequest that makes the child visible
// in the task center and ties it back to the parent receipt.
type LocaleTaskRebuilder interface {
	Build(context.Context) (publisher.BuildReport, error)
}

type localeBuildTaskReader interface {
	GetTask(string) (publisher.Task, error)
}

// localeBuildPreparer is optional so lightweight SitePublisher adapters keep
// their small Build-only contract. The production tracked publisher can expose
// it to close the crash window between the parent build receipt and the
// publisher's own durable StaticBuild child receipt.
type localeBuildPreparer interface {
	PrepareBuild(context.Context) (context.Context, error)
}

// TargetLifecycleUpdate lets the HTTP/server layer own locale-config writes
// while this package owns task state. The callback must make a best effort to
// change every listed target together; returning an error leaves the parent in
// needs-review rather than exposing an unverified locale.
type TargetLifecycleUpdate struct {
	TaskID  string   `json:"taskId"`
	Locales []string `json:"locales"`
	Status  string   `json:"status"`
}

type TargetLifecycleCallback func(context.Context, TargetLifecycleUpdate) error

// LocaleProvisionStartInput names the targets whose config entries have
// already been committed as provisioning by the caller. The task service does
// not derive targets from a request context, so an HTTP disconnect cannot
// cancel durable site-wide work.
type LocaleProvisionStartInput struct {
	Locales []string
}

type TargetTask struct {
	Locale      string             `yaml:"locale" json:"locale"`
	Status      string             `yaml:"status" json:"status"`
	Progress    taskstore.Progress `yaml:"progress" json:"progress"`
	Attempts    int                `yaml:"attempts" json:"attempts"`
	Report      *Report            `yaml:"report,omitempty" json:"report,omitempty"`
	Error       string             `yaml:"error,omitempty" json:"error,omitempty"`
	ErrorDetail string             `yaml:"errorDetail,omitempty" json:"errorDetail,omitempty"`
	StartedAt   *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
}

// Task is a durable parent receipt for one batch of newly-added/retried site
// locales. Target work remains sequential so each provider/config mutation is
// easy to recover, while successful targets share exactly one build child.
type Task struct {
	SchemaVersion int                `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string             `yaml:"id" json:"id"`
	Kind          string             `yaml:"kind" json:"kind"`
	Operation     string             `yaml:"operation" json:"operation"`
	Status        string             `yaml:"status" json:"status"`
	Progress      taskstore.Progress `yaml:"progress" json:"progress"`
	Targets       []TargetTask       `yaml:"targets" json:"targets"`
	BuildStatus   string             `yaml:"buildStatus,omitempty" json:"buildStatus,omitempty"`
	BuildTaskID   string             `yaml:"buildTaskId,omitempty" json:"buildTaskId,omitempty"`
	CreatedAt     time.Time          `yaml:"createdAt" json:"createdAt"`
	StartedAt     *time.Time         `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt   *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Error         string             `yaml:"error,omitempty" json:"error,omitempty"`
	ErrorDetail   string             `yaml:"errorDetail,omitempty" json:"errorDetail,omitempty"`
}

type TaskService struct {
	repository  *fsrepo.Repository
	provisioner Provisioner
	rebuilder   LocaleTaskRebuilder

	callbackMu      sync.RWMutex
	targetLifecycle TargetLifecycleCallback
	mutationAcquire func() func()

	root   context.Context
	cancel context.CancelFunc

	// semaphore deliberately serializes the whole batch (including its one
	// final build) rather than merely individual provider calls. That prevents
	// two independently persisted locale batches from exposing different
	// half-translated snapshots in the same public release.
	semaphore chan struct{}
	startMu   sync.Mutex
	writeMu   sync.Mutex
	runGate   sync.RWMutex

	lifecycleMu sync.Mutex
	closed      bool
	launched    map[string]struct{}
	runWG       sync.WaitGroup
	generation  atomic.Uint64

	// writeTaskHook is a narrow persistence seam for focused package tests.
	// Production leaves it nil and writes atomically through fsrepo.
	writeTaskHook func(Task) error
	// beforeRunHook is a narrow dispatch seam for focused package tests.
	// Production leaves it nil; it runs only after LaunchPrepared has captured
	// the worker generation and before the worker enters run.
	beforeRunHook func()
}

func NewTaskService(repository *fsrepo.Repository, provisioner Provisioner, rebuilder LocaleTaskRebuilder) *TaskService {
	root, cancel := context.WithCancel(context.Background())
	service := &TaskService{
		repository: repository, provisioner: provisioner, rebuilder: rebuilder,
		root: root, cancel: cancel, semaphore: make(chan struct{}, 1), launched: make(map[string]struct{}),
	}
	service.generation.Store(1)
	return service
}

// SetTargetLifecycleCallback installs the server-owned locale-config update
// hook. It is intentionally invoked only after the matching parent checkpoint
// is durable, so a crash can replay an idempotent status transition safely.
func (s *TaskService) SetTargetLifecycleCallback(callback TargetLifecycleCallback) {
	s.callbackMu.Lock()
	s.targetLifecycle = callback
	s.callbackMu.Unlock()
}

// SetMutationAcquire installs the server-wide mutation gate. The worker holds
// it for the complete Provision -> one Build -> ready/failed finalization so
// the external-change watcher cannot mistake intermediate locale files for a
// user edit and trigger an extra build.
func (s *TaskService) SetMutationAcquire(acquire func() func()) {
	s.callbackMu.Lock()
	s.mutationAcquire = acquire
	s.callbackMu.Unlock()
}

func (s *TaskService) acquireMutation() func() {
	s.callbackMu.RLock()
	acquire := s.mutationAcquire
	s.callbackMu.RUnlock()
	if acquire == nil {
		return func() {}
	}
	if release := acquire(); release != nil {
		return release
	}
	return func() {}
}

func (s *TaskService) targetStatus(ctx context.Context, taskID string, locales []string, status string) error {
	if len(locales) == 0 {
		return nil
	}
	s.callbackMu.RLock()
	callback := s.targetLifecycle
	s.callbackMu.RUnlock()
	if callback == nil {
		return nil
	}
	copyOfLocales := append([]string(nil), locales...)
	return callback(ctx, TargetLifecycleUpdate{TaskID: taskID, Locales: copyOfLocales, Status: status})
}

// Close cancels only process-owned worker contexts and waits until no worker
// can invoke a server callback. Queued/running receipts remain on disk for
// Recover on the next process start.
func (s *TaskService) Close() {
	s.lifecycleMu.Lock()
	s.closed = true
	s.lifecycleMu.Unlock()
	s.cancel()
	s.runWG.Wait()
}

// Pause is used by backup restore. Acquiring the server mutation gate before
// calling Pause preserves the same lock order as translation/scheduled work:
// mutation gate -> locale task pause gate. A runner that has not acquired the
// mutation gate cannot enter this read side while a restore is in progress.
func (s *TaskService) Pause() func() {
	s.runGate.Lock()
	s.generation.Add(1)
	var once sync.Once
	return func() { once.Do(s.runGate.Unlock) }
}

// Start persists a receipt before it schedules any provider work. Repeated
// saves with the exact same active target set return that existing receipt;
// different target sets become an independently durable, serialized batch.
func (s *TaskService) Start(input LocaleProvisionStartInput) (Task, error) {
	task, created, err := s.Prepare(input)
	if err != nil {
		return task, err
	}
	if created && !s.LaunchPrepared(task.ID) {
		slog.Warn("locale provisioning receipt persisted for startup recovery because service is closing", "task", task.ID)
	}
	return task, nil
}

// Prepare is the receipt-only half of Start. Server handlers use Start in the
// normal path; keeping this split public makes a future transactional config
// writer able to persist config and task before deciding when to launch.
func (s *TaskService) Prepare(input LocaleProvisionStartInput) (Task, bool, error) {
	targets, err := normalizeTargetLocales(input.Locales)
	if err != nil {
		return Task{}, false, err
	}
	if s == nil || s.repository == nil {
		return Task{}, false, errors.New("locale provisioning task service is unavailable")
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	tasks, err := s.List()
	if err != nil {
		return Task{}, false, err
	}
	for _, existing := range tasks {
		if (existing.Status == "queued" || existing.Status == "running") && sameTaskTargets(existing.Targets, targets) {
			return existing, false, nil
		}
	}
	id, err := newLocaleProvisionTaskID()
	if err != nil {
		return Task{}, false, err
	}
	now := time.Now().UTC()
	task := Task{
		SchemaVersion: domain.SchemaVersion,
		ID:            id,
		Kind:          localeProvisionTaskKind,
		Operation:     localeProvisionTaskOperation,
		Status:        "queued",
		Progress: taskstore.Progress{
			Phase:   "preparing-site-localization",
			Total:   len(targets) + 2,
			Message: "preparing-site-localization",
		},
		Targets:   newTargetTasks(targets),
		CreatedAt: now,
	}
	if err := s.writeTask(task); err != nil {
		return Task{}, false, err
	}
	return task, true, nil
}

// FailPrepared terminalizes a receipt that was written before the caller's
// related configuration commit, but whose configuration commit was rolled
// back. It deliberately does not invoke the target lifecycle callback: those
// config entries are no longer authoritative. Callers should invoke this only
// for a newly-created receipt, before LaunchPrepared; if persistence itself
// fails, they must keep/recover the configuration rather than pretending the
// queued receipt vanished.
func (s *TaskService) FailPrepared(taskID, errorCode string) error {
	if s == nil || !taskstore.ValidLocaleProvisionID(taskID) {
		return ErrLocaleProvisionTaskNotFound
	}
	errorCode = strings.TrimSpace(errorCode)
	if errorCode == "" {
		errorCode = "locale-config-commit-failed"
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	task, err := s.Get(taskID)
	if err != nil {
		return err
	}
	if task.Status != "queued" {
		return ErrLocaleProvisionTaskNotQueued
	}
	now := time.Now().UTC()
	for index := range task.Targets {
		target := &task.Targets[index]
		if target.Status != "queued" {
			return ErrLocaleProvisionTaskNotQueued
		}
		target.Status = "failed"
		target.Error = errorCode
		target.ErrorDetail = ""
		target.CompletedAt = &now
		target.Progress = taskstore.Advance(target.Progress, "preparing-site-localization", 0, 1, 0, errorCode)
	}
	return s.finish(&task, "failed", errorCode)
}

// Launch is an alias that makes the lifecycle API convenient to callers that
// obtained a receipt through Prepare.
func (s *TaskService) Launch(taskID string) bool { return s.LaunchPrepared(taskID) }

// LaunchPrepared starts a stored receipt at most once per process. run always
// rereads the task, so a crash after Prepare but before this call is recovered
// without trusting a stale in-memory copy.
func (s *TaskService) LaunchPrepared(taskID string) bool {
	if !taskstore.ValidLocaleProvisionID(taskID) {
		return false
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
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
	// Capture the generation synchronously with the successful launch, rather
	// than in the goroutine. Otherwise a restore can complete after LaunchPrepared
	// returns but before the goroutine first runs, allowing it to observe the new
	// generation and incorrectly treat the old receipt as current.
	runGeneration := s.generation.Load()
	beforeRun := s.beforeRunHook
	s.lifecycleMu.Unlock()
	go func() {
		defer s.runWG.Done()
		defer func() {
			s.lifecycleMu.Lock()
			delete(s.launched, taskID)
			s.lifecycleMu.Unlock()
		}()
		if beforeRun != nil {
			beforeRun()
		}
		s.run(taskID, runGeneration)
	}()
	return true
}

// Recover queues every interrupted batch. The publisher is expected to finish
// its own interrupted-child recovery first; if a child is already succeeded,
// run finalizes ready state without issuing another static build.
func (s *TaskService) Recover() (int, error) {
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if s.LaunchPrepared(task.ID) {
			recovered++
		}
	}
	return recovered, nil
}

func (s *TaskService) run(taskID string, runGeneration uint64) {
	if s.root.Err() != nil {
		return
	}
	select {
	case s.semaphore <- struct{}{}:
	case <-s.root.Done():
		return
	}
	defer func() { <-s.semaphore }()

	// Do not wait for the shared mutation gate while holding the pause read
	// lock. Restore takes the mutation gate first and then Pause, so this order
	// avoids the otherwise possible lock inversion.
	releaseMutation := s.acquireMutation()
	defer releaseMutation()
	s.runGate.RLock()
	defer s.runGate.RUnlock()
	if s.root.Err() != nil {
		return
	}
	task, err := s.Get(taskID)
	if err != nil || (task.Status != "queued" && task.Status != "running") {
		return
	}
	if !s.generationCurrent(runGeneration) {
		s.finishBestEffort(&task, "needs-review", "source-changed")
		return
	}
	if task.Status == "queued" {
		now := time.Now().UTC()
		task.Status = "running"
		task.StartedAt = &now
		task.Progress = taskstore.Advance(task.Progress, "preparing-site-localization", 0, taskTotal(&task), 1, "preparing-site-localization")
		if !s.checkpoint(&task, "start") {
			return
		}
	}
	if !s.generationCurrent(runGeneration) {
		s.finishBestEffort(&task, "needs-review", "source-changed")
		return
	}
	if task.BuildTaskID != "" || task.BuildStatus == "succeeded" {
		s.resumeBuild(&task, runGeneration)
		return
	}

	for index := range task.Targets {
		if s.root.Err() != nil {
			return
		}
		if !s.generationCurrent(runGeneration) {
			s.finishBestEffort(&task, "needs-review", "source-changed")
			return
		}
		target := &task.Targets[index]
		switch target.Status {
		case "succeeded", "failed", "needs-review":
			continue
		case "queued", "running":
		default:
			s.finishBestEffort(&task, "needs-review", "locale-target-unavailable")
			return
		}
		now := time.Now().UTC()
		target.Status = "running"
		target.Attempts++
		target.StartedAt = &now
		target.Error = ""
		target.ErrorDetail = ""
		target.Progress = taskstore.Advance(target.Progress, "localizing-site", 0, 1, 5, "localizing-site")
		task.Progress = taskstore.Advance(task.Progress, "localizing-site", completedTargetCount(task.Targets), taskTotal(&task), task.Progress.Percent, "localizing-site")
		if !s.checkpoint(&task, "target-start") {
			return
		}
		if s.provisioner == nil {
			s.recordTargetFailure(&task, index, "failed", "site-localization-failed", "")
			if !s.checkpoint(&task, "target-unavailable") {
				return
			}
			if err := s.targetStatus(s.root, task.ID, []string{target.Locale}, "failed"); err != nil {
				if s.root.Err() != nil {
					return
				}
				s.finishBestEffort(&task, "needs-review", "locale-status-update-failed")
				return
			}
			continue
		}

		report, provisionErr := s.provisioner.Provision(s.root, target.Locale)
		if s.root.Err() != nil {
			return
		}
		if !s.generationCurrent(runGeneration) {
			s.finishBestEffort(&task, "needs-review", "source-changed")
			return
		}
		if provisionErr != nil {
			status, message, detail := provisionFailure(provisionErr)
			s.recordTargetFailure(&task, index, status, message, detail)
			if detail != "" {
				slog.Warn("locale provisioning target failed", "task", task.ID, "locale", target.Locale, "reason", message, "detail", detail)
			}
			if !s.checkpoint(&task, "target-failed") {
				return
			}
			if err := s.targetStatus(s.root, task.ID, []string{target.Locale}, "failed"); err != nil {
				if s.root.Err() != nil {
					return
				}
				s.finishBestEffort(&task, "needs-review", "locale-status-update-failed")
				return
			}
			// A source/generation fence means this parent no longer describes a
			// coherent snapshot. Do not translate remaining targets or build the
			// subset: a fresh explicit retry must plan against the new source.
			if status == "needs-review" {
				s.finishBestEffort(&task, "needs-review", message)
				return
			}
			continue
		}
		completed := time.Now().UTC()
		target.Status = "succeeded"
		target.Report = &report
		target.CompletedAt = &completed
		target.Error = ""
		target.Progress = taskstore.Complete(target.Progress, localeProvisionTargetComplete)
		task.Progress = taskstore.Advance(task.Progress, "localizing-site", completedTargetCount(task.Targets), taskTotal(&task), task.Progress.Percent, "localizing-site")
		if !s.checkpoint(&task, "target-succeeded") {
			return
		}
	}

	if !s.generationCurrent(runGeneration) {
		s.finishBestEffort(&task, "needs-review", "source-changed")
		return
	}
	successful := successfulTargetLocales(task.Targets)
	if len(successful) == 0 {
		s.finishFromTargetResults(&task)
		return
	}
	if err := s.targetStatus(s.root, task.ID, successful, "building"); err != nil {
		if s.root.Err() != nil {
			return
		}
		s.finishBestEffort(&task, "needs-review", "locale-status-update-failed")
		return
	}
	buildTaskID, err := publisher.NewBuildTaskID()
	if err != nil {
		s.finishBestEffort(&task, "failed", "localization-build-failed")
		return
	}
	task.BuildTaskID = buildTaskID
	task.BuildStatus = "queued"
	task.Progress = taskstore.Advance(task.Progress, "starting-localization-build", completedTargetCount(task.Targets), taskTotal(&task), 70, "starting-localization-build")
	if !s.checkpoint(&task, "build-receipt") {
		return
	}
	s.resumeBuild(&task, runGeneration)
}

func (s *TaskService) resumeBuild(task *Task, runGeneration uint64) {
	if !s.generationCurrent(runGeneration) {
		s.finishBestEffort(task, "needs-review", "source-changed")
		return
	}
	successful := successfulTargetLocales(task.Targets)
	if len(successful) == 0 {
		s.finishFromTargetResults(task)
		return
	}
	if task.BuildStatus == "succeeded" {
		s.finalizeSuccessfulBuild(task, successful, runGeneration)
		return
	}
	if task.BuildStatus == "failed" {
		// The parent has already durably observed a failed child. Retrying a
		// build belongs to a new explicit locale-provision attempt, never to
		// recovery of this receipt.
		s.finalizeFailedBuild(task, successful)
		return
	}
	if task.BuildTaskID == "" {
		s.finishBestEffort(task, "needs-review", "build-child-incomplete")
		return
	}
	if reader, ok := s.rebuilder.(localeBuildTaskReader); ok {
		child, err := reader.GetTask(task.BuildTaskID)
		if err == nil {
			switch child.Status {
			case "succeeded":
				task.BuildStatus = "succeeded"
				task.Progress = taskstore.Advance(task.Progress, "finalizing-localized-site", completedTargetCount(task.Targets)+1, taskTotal(task), 95, "finalizing-localized-site")
				if !s.checkpoint(task, "build-already-succeeded") {
					return
				}
				s.finalizeSuccessfulBuild(task, successful, runGeneration)
				return
			case "failed":
				task.BuildStatus = "failed"
				if !s.checkpoint(task, "build-already-failed") {
					return
				}
				s.finalizeFailedBuild(task, successful)
				return
			case "queued", "running":
				// A fresh process should see publisher.Recover terminalize its
				// children before locale task recovery. Never issue a second build
				// while the first receipt is still unresolved.
				s.finishBestEffort(task, "needs-review", "build-child-incomplete")
				return
			}
		}
		// A queued parent receipt is deliberately written before Build has a
		// chance to create its own child file. If that exact child does not
		// exist yet, using the same ID is the normal first execution, not a
		// duplicate. Once the parent was marked running, however, the missing
		// child leaves an unknown activation state and must fail closed.
		if task.BuildStatus == "running" {
			s.finishBestEffort(task, "needs-review", "build-child-incomplete")
			return
		}
	}
	if s.rebuilder == nil {
		task.BuildStatus = "failed"
		if !s.checkpoint(task, "build-unavailable") {
			return
		}
		s.finalizeFailedBuild(task, successful)
		return
	}

	buildContext := publisher.WithBuildRequest(s.root, publisher.BuildRequest{
		TaskID:       task.BuildTaskID,
		Operation:    localeProvisionTaskOperation,
		SubjectKind:  "Site",
		SubjectID:    "locales",
		ParentTaskID: task.ID,
	})
	// The parent receipt (including its exact child ID) was durably written
	// before reaching this call. When the publisher exposes PrepareBuild, make
	// its own StaticBuild child durable before recording the parent as running;
	// after a crash Recover can then inspect a concrete child instead of
	// guessing whether rendering ever began.
	if preparer, ok := s.rebuilder.(localeBuildPreparer); ok {
		prepared, prepareErr := preparer.PrepareBuild(buildContext)
		if prepareErr != nil {
			if s.root.Err() != nil {
				return
			}
			task.BuildStatus = "failed"
			if !s.checkpoint(task, "build-prepare-failed") {
				return
			}
			s.finalizeFailedBuild(task, successful)
			return
		}
		buildContext = prepared
	}
	task.BuildStatus = "running"
	task.Progress = taskstore.Advance(task.Progress, "building-localized-site", completedTargetCount(task.Targets), taskTotal(task), 78, "building-localized-site")
	if !s.checkpoint(task, "build-start") {
		return
	}
	if _, err := s.rebuilder.Build(buildContext); err != nil {
		if s.root.Err() != nil {
			return
		}
		task.BuildStatus = "failed"
		if !s.checkpoint(task, "build-failed") {
			return
		}
		s.finalizeFailedBuild(task, successful)
		return
	}
	if s.root.Err() != nil {
		return
	}
	if !s.generationCurrent(runGeneration) {
		s.finishBestEffort(task, "needs-review", "source-changed")
		return
	}
	task.BuildStatus = "succeeded"
	task.Progress = taskstore.Advance(task.Progress, "finalizing-localized-site", completedTargetCount(task.Targets)+1, taskTotal(task), 95, "finalizing-localized-site")
	if !s.checkpoint(task, "build-succeeded") {
		return
	}
	s.finalizeSuccessfulBuild(task, successful, runGeneration)
}

func (s *TaskService) finalizeSuccessfulBuild(task *Task, successful []string, runGeneration uint64) {
	if !s.generationCurrent(runGeneration) {
		s.finishBestEffort(task, "needs-review", "source-changed")
		return
	}
	if err := s.targetStatus(s.root, task.ID, successful, "ready"); err != nil {
		if s.root.Err() != nil {
			return
		}
		// The public build is already active but config visibility was not
		// confirmed. Do not rebuild; an explicit review/retry can safely replay
		// only the ready callback from the durable succeeded child receipt.
		s.finishBestEffort(task, "needs-review", "locale-status-update-failed")
		return
	}
	if s.root.Err() != nil {
		return
	}
	s.finishFromTargetResults(task)
}

func (s *TaskService) finalizeFailedBuild(task *Task, successful []string) {
	if err := s.targetStatus(s.root, task.ID, successful, "failed"); err != nil {
		if s.root.Err() != nil {
			return
		}
		s.finishBestEffort(task, "needs-review", "locale-status-update-failed")
		return
	}
	if s.root.Err() != nil {
		return
	}
	s.finishBestEffort(task, "failed", "localization-build-failed")
}

func (s *TaskService) recordTargetFailure(task *Task, index int, status, message, detail string) {
	now := time.Now().UTC()
	target := &task.Targets[index]
	target.Status = status
	target.Error = message
	target.ErrorDetail = detail
	target.CompletedAt = &now
	target.Progress = taskstore.Advance(target.Progress, "localizing-site", target.Progress.Current, 1, target.Progress.Percent, message)
	task.Progress = taskstore.Advance(task.Progress, "localizing-site", completedTargetCount(task.Targets), taskTotal(task), task.Progress.Percent, message)
}

func (s *TaskService) finishFromTargetResults(task *Task) {
	if s.root.Err() != nil {
		return
	}
	needsReview := false
	failed := false
	for _, target := range task.Targets {
		switch target.Status {
		case "needs-review":
			needsReview = true
		case "failed":
			failed = true
		}
	}
	switch {
	case needsReview:
		s.finishBestEffort(task, "needs-review", "source-changed")
	case failed:
		message, detail := localeProvisionFailureSummary(task.Targets)
		s.finishBestEffortWithDetail(task, "failed", message, detail)
	default:
		s.finishBestEffort(task, "succeeded", "site-localization-complete")
	}
}

func (s *TaskService) checkpoint(task *Task, checkpoint string) bool {
	if err := s.writeTask(*task); err == nil {
		return true
	} else {
		// A previous durable checkpoint remains queued/running, which is safer
		// than making more mutations whose ownership could not be recorded.
		slog.Error("persist locale provisioning checkpoint failed; task remains recoverable", "task", task.ID, "checkpoint", checkpoint, "error", err)
		return false
	}
}

func (s *TaskService) finishBestEffort(task *Task, status, message string) {
	s.finishBestEffortWithDetail(task, status, message, "")
}

func (s *TaskService) finishBestEffortWithDetail(task *Task, status, message, detail string) {
	if err := s.finishWithDetail(task, status, message, detail); err != nil {
		slog.Error("persist terminal locale provisioning task failed; task remains recoverable", "task", task.ID, "status", status, "error", err)
	}
}

func (s *TaskService) finish(task *Task, status, message string) error {
	return s.finishWithDetail(task, status, message, "")
}

func (s *TaskService) finishWithDetail(task *Task, status, message, detail string) error {
	now := time.Now().UTC()
	task.Status = status
	task.CompletedAt = &now
	task.Error = ""
	task.ErrorDetail = ""
	if status == "succeeded" {
		task.Progress = taskstore.Complete(task.Progress, "site-localization-complete")
	} else {
		task.Error = message
		task.ErrorDetail = detail
		task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, task.Progress.Current, taskTotal(task), task.Progress.Percent, message)
	}
	return s.writeTask(*task)
}

func (s *TaskService) generationCurrent(generation uint64) bool {
	return generation != 0 && generation == s.generation.Load()
}

// List returns only locale-provision parents but validates every shared task
// directory entry first. This matches other durable task readers: a corrupt
// foreign record is surfaced rather than silently skipped.
func (s *TaskService) List() ([]Task, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("locale provisioning task service is unavailable")
	}
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
		if err := s.repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return nil, fmt.Errorf("read locale provisioning task %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("locale provisioning task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != localeProvisionTaskKind {
			continue
		}
		if !validLocaleProvisionTask(task, expectedID) {
			return nil, fmt.Errorf("locale provisioning task %q is invalid", expectedID)
		}
		normalizeLocaleProvisionTask(&task)
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks, nil
}

func (s *TaskService) Get(id string) (Task, error) {
	if s == nil || s.repository == nil || !taskstore.ValidLocaleProvisionID(id) {
		return Task{}, ErrLocaleProvisionTaskNotFound
	}
	var task Task
	if err := s.repository.ReadYAML(filepath.Join("state", "tasks", id+".yaml"), &task); err != nil || !validLocaleProvisionTask(task, id) {
		return Task{}, ErrLocaleProvisionTaskNotFound
	}
	normalizeLocaleProvisionTask(&task)
	return task, nil
}

func (s *TaskService) writeTask(task Task) error {
	if !taskstore.ValidLocaleProvisionID(task.ID) {
		return ErrLocaleProvisionTaskNotFound
	}
	var lastErr error
	for attempt := 1; attempt <= localeProvisionWriteAttempts; attempt++ {
		lastErr = s.writeTaskOnce(task)
		if lastErr == nil {
			return nil
		}
		if attempt < localeProvisionWriteAttempts {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *TaskService) writeTaskOnce(task Task) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeTaskHook != nil {
		if err := s.writeTaskHook(task); err != nil {
			return err
		}
	}
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
}

func newLocaleProvisionTaskID() (string, error) {
	buildID, err := publisher.NewBuildTaskID()
	if err != nil {
		return "", err
	}
	return localeProvisionTaskIDPrefix + buildID, nil
}

func normalizeTargetLocales(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, ErrLocaleProvisionTaskInput
	}
	seen := make(map[string]bool, len(raw))
	targets := make([]string, 0, len(raw))
	for _, value := range raw {
		tag, err := language.Parse(strings.TrimSpace(value))
		if err != nil || strings.TrimSpace(value) == "" || tag.String() == localeProvisionSourceLocale {
			return nil, ErrLocaleProvisionTaskInput
		}
		locale := tag.String()
		if seen[locale] {
			continue
		}
		seen[locale] = true
		targets = append(targets, locale)
	}
	sort.Strings(targets)
	if len(targets) == 0 {
		return nil, ErrLocaleProvisionTaskInput
	}
	return targets, nil
}

func newTargetTasks(locales []string) []TargetTask {
	targets := make([]TargetTask, 0, len(locales))
	for _, locale := range locales {
		targets = append(targets, TargetTask{
			Locale: locale, Status: "queued",
			Progress: taskstore.Progress{Phase: "preparing-site-localization", Total: 1, Message: "preparing-site-localization"},
		})
	}
	return targets
}

func sameTaskTargets(existing []TargetTask, targets []string) bool {
	if len(existing) != len(targets) {
		return false
	}
	for index, target := range existing {
		if target.Locale != targets[index] {
			return false
		}
	}
	return true
}

func successfulTargetLocales(targets []TargetTask) []string {
	locales := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Status == "succeeded" {
			locales = append(locales, target.Locale)
		}
	}
	return locales
}

func completedTargetCount(targets []TargetTask) int {
	completed := 0
	for _, target := range targets {
		if target.Status == "succeeded" || target.Status == "failed" || target.Status == "needs-review" {
			completed++
		}
	}
	return completed
}

func taskTotal(task *Task) int {
	if task.Progress.Total > 0 {
		return task.Progress.Total
	}
	return len(task.Targets) + 2
}

func provisionFailure(err error) (status, message, detail string) {
	if errors.Is(err, ErrSourceChanged) || errors.Is(err, content.ErrSourceChanged) || errors.Is(err, content.ErrTargetChanged) {
		return "needs-review", "source-changed", ""
	}
	switch {
	case errors.Is(err, ai.ErrKeyMissing):
		return "failed", "provider-key-missing", ""
	case errors.Is(err, ai.ErrProviderFailed):
		return "failed", "provider-request-failed", safeProviderFailureDetail(err)
	case errors.Is(err, ai.ErrInvalidProvider), errors.Is(err, ai.ErrProviderNotFound):
		return "failed", "provider-unavailable", ""
	case errors.Is(err, ai.ErrTranslationInputTooLong):
		return "failed", "provider-input-too-large", ""
	case errors.Is(err, ErrLocaleUnavailable):
		return "failed", "locale-target-unavailable", ""
	default:
		return "failed", "site-localization-failed", ""
	}
}

// safeProviderFailureDetail contains only the bounded, secret-redacted detail
// emitted by the AI client. Never surface arbitrary wrapped error text here:
// localization errors can contain private site content or filesystem paths.
func safeProviderFailureDetail(err error) string {
	message := strings.TrimSpace(ai.SafeProviderErrorMessage(err))
	if detail, found := strings.CutPrefix(message, ai.ErrProviderFailed.Error()+":"); found {
		return strings.TrimSpace(detail)
	}
	return ""
}

func localeProvisionFailureSummary(targets []TargetTask) (message, detail string) {
	message = "site-localization-failed"
	found := false
	for _, target := range targets {
		if target.Status != "failed" || target.Error == "" {
			continue
		}
		if !found {
			found = true
			message = target.Error
			detail = target.ErrorDetail
			continue
		}
		if target.Error != message {
			return "site-localization-failed", ""
		}
		if target.ErrorDetail != detail {
			detail = ""
		}
	}
	return message, detail
}

func validLocaleProvisionTask(task Task, expectedID string) bool {
	if task.SchemaVersion != domain.SchemaVersion || task.ID != expectedID || task.Kind != localeProvisionTaskKind || !taskstore.ValidLocaleProvisionID(task.ID) || task.Operation != localeProvisionTaskOperation || task.CreatedAt.IsZero() || len(task.Targets) == 0 {
		return false
	}
	switch task.Status {
	case "queued", "running", "succeeded", "failed", "needs-review":
	default:
		return false
	}
	switch task.BuildStatus {
	case "":
		if task.BuildTaskID != "" {
			return false
		}
	case "queued", "running", "succeeded", "failed":
		if !taskstore.ValidStaticBuildID(task.BuildTaskID) {
			return false
		}
	default:
		return false
	}
	seen := make(map[string]bool, len(task.Targets))
	previousLocale := ""
	for _, target := range task.Targets {
		tag, err := language.Parse(target.Locale)
		if err != nil || target.Locale != tag.String() || target.Locale == localeProvisionSourceLocale || seen[target.Locale] || (previousLocale != "" && target.Locale <= previousLocale) || target.Attempts < 0 {
			return false
		}
		seen[target.Locale] = true
		previousLocale = target.Locale
		switch target.Status {
		case "queued", "running", "succeeded", "failed", "needs-review":
		default:
			return false
		}
	}
	return true
}

func normalizeLocaleProvisionTask(task *Task) {
	if task.Progress.Total < 1 {
		task.Progress.Total = len(task.Targets) + 2
	}
	if task.Progress.Phase == "" {
		task.Progress.Phase = task.Status
	}
	if task.Status == "succeeded" && task.Progress.Percent < 100 {
		task.Progress = taskstore.Complete(task.Progress, "site-localization-complete")
	}
	for index := range task.Targets {
		target := &task.Targets[index]
		if target.Progress.Total < 1 {
			target.Progress.Total = 1
		}
		if target.Progress.Phase == "" {
			target.Progress.Phase = target.Status
		}
		if target.Status == "succeeded" && target.Progress.Percent < 100 {
			target.Progress = taskstore.Complete(target.Progress, localeProvisionTargetComplete)
		}
	}
}
