package localization

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

type localeTaskProvisioner struct {
	mu       sync.Mutex
	calls    []string
	failures map[string]error
	started  chan struct{}
	release  chan struct{}
}

func (p *localeTaskProvisioner) Provision(ctx context.Context, locale string) (Report, error) {
	p.mu.Lock()
	p.calls = append(p.calls, locale)
	started := p.started
	release := p.release
	failure := p.failures[locale]
	p.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return Report{}, ctx.Err()
		}
	}
	if failure != nil {
		return Report{}, failure
	}
	return Report{Locale: locale, Posts: 1}, nil
}

func (p *localeTaskProvisioner) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

type localeTaskBuilder struct {
	mu       sync.Mutex
	calls    int
	request  publisher.BuildRequest
	failure  error
	children map[string]publisher.Task
}

func (b *localeTaskBuilder) Build(ctx context.Context) (publisher.BuildReport, error) {
	request := publisher.BuildRequestFromContext(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	b.request = request
	if b.failure != nil {
		b.children[request.TaskID] = publisher.Task{SchemaVersion: domain.SchemaVersion, ID: request.TaskID, Kind: "StaticBuild", Status: "failed", ParentTaskID: request.ParentTaskID}
		return publisher.BuildReport{}, b.failure
	}
	b.children[request.TaskID] = publisher.Task{SchemaVersion: domain.SchemaVersion, ID: request.TaskID, Kind: "StaticBuild", Status: "succeeded", ParentTaskID: request.ParentTaskID}
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
}

func (b *localeTaskBuilder) GetTask(id string) (publisher.Task, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	task, exists := b.children[id]
	if !exists {
		return publisher.Task{}, publisher.ErrBuildTaskNotFound
	}
	return task, nil
}

func (b *localeTaskBuilder) snapshot() (int, publisher.BuildRequest) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls, b.request
}

func localeTaskRepository(t *testing.T) *fsrepo.Repository {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func waitLocaleTask(t *testing.T, service *TaskService, id string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := service.Get(id)
		if err == nil && (task.Status == "succeeded" || task.Status == "failed" || task.Status == "needs-review") {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	task, err := service.Get(id)
	t.Fatalf("locale task %s did not reach terminal state: %#v, %v", id, task, err)
	return Task{}
}

func TestLocaleProvisionTaskPersistsBeforeLaunchAndReusesExactActiveTargets(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{started: make(chan struct{}, 1), release: make(chan struct{})}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-provisioner.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	stored, err := service.Get(task.ID)
	if err != nil || stored.Status != "running" || stored.Targets[0].Status != "running" {
		t.Fatalf("durable task before provider returns = %#v, %v", stored, err)
	}
	reused, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil || reused.ID != task.ID {
		t.Fatalf("active identical start = %#v, %v, want %s", reused, err, task.ID)
	}
	close(provisioner.release)
	if completed := waitLocaleTask(t, service, task.ID); completed.Status != "succeeded" {
		t.Fatalf("completed task = %#v", completed)
	}
}

func TestLocaleProvisionTaskRunsTargetsSequentiallyAndBuildsOnce(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()
	var lifecycleMu sync.Mutex
	var lifecycle []TargetLifecycleUpdate
	service.SetTargetLifecycleCallback(func(_ context.Context, update TargetLifecycleUpdate) error {
		lifecycleMu.Lock()
		lifecycle = append(lifecycle, update)
		lifecycleMu.Unlock()
		return nil
	})

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja", "en"}})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "succeeded" || completed.BuildStatus != "succeeded" || completed.BuildTaskID == "" {
		t.Fatalf("completed parent = %#v", completed)
	}
	if calls := provisioner.Calls(); len(calls) != 2 || calls[0] != "en" || calls[1] != "ja" {
		t.Fatalf("sequential provision calls = %#v", calls)
	}
	builds, request := builder.snapshot()
	if builds != 1 || request.TaskID != completed.BuildTaskID || request.ParentTaskID != completed.ID || request.Operation != localeProvisionTaskOperation {
		t.Fatalf("build child receipt = calls:%d request:%#v parent:%#v", builds, request, completed)
	}
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if len(lifecycle) != 2 || lifecycle[0].Status != "building" || lifecycle[1].Status != "ready" || lifecycle[0].TaskID != completed.ID || lifecycle[1].TaskID != completed.ID {
		t.Fatalf("lifecycle updates = %#v", lifecycle)
	}
	if len(lifecycle[0].Locales) != 2 || lifecycle[0].Locales[0] != "en" || lifecycle[0].Locales[1] != "ja" {
		t.Fatalf("building locales = %#v", lifecycle[0])
	}
}

func TestLocaleProvisionTaskLeavesFailedTargetsHiddenAndBuildsSuccessfulTargets(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{failures: map[string]error{"ja": errors.New("provider failed")}}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()
	var lifecycleMu sync.Mutex
	var lifecycle []TargetLifecycleUpdate
	service.SetTargetLifecycleCallback(func(_ context.Context, update TargetLifecycleUpdate) error {
		lifecycleMu.Lock()
		lifecycle = append(lifecycle, update)
		lifecycleMu.Unlock()
		return nil
	})

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"en", "ja"}})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "failed" || completed.Error != "site-localization-failed" || completed.BuildStatus != "succeeded" {
		t.Fatalf("partial task = %#v", completed)
	}
	if completed.Targets[0].Locale != "en" || completed.Targets[0].Status != "succeeded" || completed.Targets[1].Locale != "ja" || completed.Targets[1].Status != "failed" {
		t.Fatalf("target terminal states = %#v", completed.Targets)
	}
	builds, _ := builder.snapshot()
	if builds != 1 {
		t.Fatalf("successful target build calls = %d", builds)
	}
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if len(lifecycle) != 3 || lifecycle[0].Status != "failed" || lifecycle[0].Locales[0] != "ja" || lifecycle[1].Status != "building" || lifecycle[2].Status != "ready" {
		t.Fatalf("partial lifecycle = %#v", lifecycle)
	}
}

func TestLocaleProvisionTaskSourceChangeDoesNotBuildSubset(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{failures: map[string]error{"en": content.ErrSourceChanged}}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"en", "ja"}})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "needs-review" || completed.Error != "source-changed" || completed.BuildTaskID != "" {
		t.Fatalf("source changed task = %#v", completed)
	}
	builds, _ := builder.snapshot()
	if builds != 0 {
		t.Fatalf("source-changed task built %d times", builds)
	}
}

func TestLocaleProvisionTaskFencesWorkerQueuedBehindRestoreMutationGate(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()

	waitingForMutation := make(chan struct{}, 1)
	releaseMutation := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseMutation) })
	}
	defer release()
	service.SetMutationAcquire(func() func() {
		select {
		case waitingForMutation <- struct{}{}:
		default:
		}
		<-releaseMutation
		return func() {}
	})

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-waitingForMutation:
	case <-time.After(time.Second):
		t.Fatal("task did not wait for the mutation gate")
	}

	// Model a backup restore: it advances the generation while the old worker
	// is already launched but blocked before it can take the pause read lock.
	resume := service.Pause()
	resume()
	release()

	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "needs-review" || completed.Error != "source-changed" {
		t.Fatalf("generation-fenced task = %#v", completed)
	}
	if calls := provisioner.Calls(); len(calls) != 0 {
		t.Fatalf("generation-fenced task provisioned targets = %#v", calls)
	}
	if builds, _ := builder.snapshot(); builds != 0 {
		t.Fatalf("generation-fenced task built %d times", builds)
	}
}

func TestLocaleProvisionTaskFencesWorkerNotYetRunWhenRestoreStarts(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()

	beforeRun := make(chan struct{}, 1)
	releaseRun := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseRun) })
	}
	defer release()
	service.beforeRunHook = func() {
		select {
		case beforeRun <- struct{}{}:
		default:
		}
		<-releaseRun
	}

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-beforeRun:
	case <-time.After(time.Second):
		t.Fatal("launched worker did not stop before run")
	}

	// Launch has returned, but the worker has not entered run. A restore at
	// this point must fence the launch generation captured by LaunchPrepared.
	resume := service.Pause()
	resume()
	release()

	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "needs-review" || completed.Error != "source-changed" {
		t.Fatalf("pre-run generation-fenced task = %#v", completed)
	}
	if calls := provisioner.Calls(); len(calls) != 0 {
		t.Fatalf("pre-run generation-fenced task provisioned targets = %#v", calls)
	}
	if builds, _ := builder.snapshot(); builds != 0 {
		t.Fatalf("pre-run generation-fenced task built %d times", builds)
	}
}

func TestLocaleProvisionRecoveryFinalizesSucceededBuildWithoutSecondBuild(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()
	var lifecycle []TargetLifecycleUpdate
	service.SetTargetLifecycleCallback(func(_ context.Context, update TargetLifecycleUpdate) error {
		lifecycle = append(lifecycle, update)
		return nil
	})

	task, created, err := service.Prepare(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil || !created {
		t.Fatalf("prepare = %#v, %t, %v", task, created, err)
	}
	childID, err := publisher.NewBuildTaskID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task.Status = "running"
	task.Targets[0].Status = "succeeded"
	task.Targets[0].Report = &Report{Locale: "ja"}
	task.Targets[0].CompletedAt = &now
	task.BuildTaskID = childID
	task.BuildStatus = "running"
	builder.children[childID] = publisher.Task{SchemaVersion: domain.SchemaVersion, ID: childID, Kind: "StaticBuild", Status: "succeeded", ParentTaskID: task.ID}
	if err := service.writeTask(task); err != nil {
		t.Fatal(err)
	}
	if recovered, err := service.Recover(); err != nil || recovered != 1 {
		t.Fatalf("Recover() = %d, %v", recovered, err)
	}
	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "succeeded" || completed.BuildStatus != "succeeded" {
		t.Fatalf("recovered parent = %#v", completed)
	}
	builds, _ := builder.snapshot()
	if builds != 0 {
		t.Fatalf("recovery duplicated succeeded child: %d builds", builds)
	}
	if len(lifecycle) != 1 || lifecycle[0].Status != "ready" || lifecycle[0].Locales[0] != "ja" {
		t.Fatalf("recovery lifecycle = %#v", lifecycle)
	}
}

func TestLocaleProvisionTaskCallbackFailureIsTerminalAndDoesNotBuild(t *testing.T) {
	repository := localeTaskRepository(t)
	provisioner := &localeTaskProvisioner{}
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, provisioner, builder)
	defer service.Close()
	service.SetTargetLifecycleCallback(func(_ context.Context, update TargetLifecycleUpdate) error {
		if update.Status == "building" {
			return errors.New("write config failed")
		}
		return nil
	})

	task, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitLocaleTask(t, service, task.ID)
	if completed.Status != "needs-review" || completed.Error != "locale-status-update-failed" || completed.BuildTaskID != "" {
		t.Fatalf("callback failure task = %#v", completed)
	}
	builds, _ := builder.snapshot()
	if builds != 0 {
		t.Fatalf("callback failure build calls = %d", builds)
	}
}

func TestLocaleProvisionTaskReceiptWriteFailureLeavesNoTask(t *testing.T) {
	repository := localeTaskRepository(t)
	service := NewTaskService(repository, &localeTaskProvisioner{}, &localeTaskBuilder{children: make(map[string]publisher.Task)})
	defer service.Close()
	service.writeTaskHook = func(Task) error { return errors.New("disk unavailable") }
	if _, err := service.Start(LocaleProvisionStartInput{Locales: []string{"ja"}}); err == nil {
		t.Fatal("Start unexpectedly succeeded")
	}
	service.writeTaskHook = nil
	tasks, err := service.List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("receipt failure task list = %#v, %v", tasks, err)
	}
}

func TestLocaleProvisionTaskFailPreparedStopsRecoveryWithoutLifecycleCallback(t *testing.T) {
	repository := localeTaskRepository(t)
	builder := &localeTaskBuilder{children: make(map[string]publisher.Task)}
	service := NewTaskService(repository, &localeTaskProvisioner{}, builder)
	defer service.Close()
	callbackCalls := 0
	service.SetTargetLifecycleCallback(func(context.Context, TargetLifecycleUpdate) error {
		callbackCalls++
		return nil
	})

	task, created, err := service.Prepare(LocaleProvisionStartInput{Locales: []string{"ja"}})
	if err != nil || !created {
		t.Fatalf("Prepare() = %#v, %t, %v", task, created, err)
	}
	if err := service.FailPrepared(task.ID, "locale-config-commit-failed"); err != nil {
		t.Fatal(err)
	}
	stored, err := service.Get(task.ID)
	if err != nil || stored.Status != "failed" || stored.Error != "locale-config-commit-failed" || stored.Targets[0].Status != "failed" {
		t.Fatalf("failed prepared receipt = %#v, %v", stored, err)
	}
	if recovered, err := service.Recover(); err != nil || recovered != 0 || callbackCalls != 0 {
		t.Fatalf("failed prepared recovery = %d, %v; callbacks = %d", recovered, err, callbackCalls)
	}
	if builds, _ := builder.snapshot(); builds != 0 {
		t.Fatalf("failed prepared receipt launched a build: %d", builds)
	}
}

func TestLocaleProvisionTaskListSkipsValidatedForeignTaskKinds(t *testing.T) {
	repository := localeTaskRepository(t)
	service := NewTaskService(repository, &localeTaskProvisioner{}, &localeTaskBuilder{children: make(map[string]publisher.Task)})
	defer service.Close()
	if err := repository.WriteYAML("state/tasks/translation-foreign.yaml", taskstore.Header{
		SchemaVersion: domain.SchemaVersion,
		ID:            "translation-foreign",
		Kind:          "Translation",
	}, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("List() with valid foreign task = %#v, %v", tasks, err)
	}
}
