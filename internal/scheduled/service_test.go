package scheduled

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type countingRebuilder struct {
	calls atomic.Int32
	err   error
}

func (rebuilder *countingRebuilder) Build(context.Context) (publisher.BuildReport, error) {
	rebuilder.calls.Add(1)
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, rebuilder.err
}

type countingTranslator struct {
	prepareCalls atomic.Int32
	launchCalls  atomic.Int32
	err          error
	mu           sync.RWMutex
	task         translation.Task
	input        translation.StartInput
}

func (translator *countingTranslator) Prepare(input translation.StartInput) (translation.Task, bool, error) {
	translator.prepareCalls.Add(1)
	task := translation.Task{ID: "translation-scheduled-test", Status: "queued"}
	translator.mu.Lock()
	translator.input = input
	translator.mu.Unlock()
	if translator.err == nil {
		translator.mu.Lock()
		translator.task = task
		translator.mu.Unlock()
	}
	return task, true, translator.err
}

func (translator *countingTranslator) LaunchPrepared(string) bool {
	translator.launchCalls.Add(1)
	return true
}

func (translator *countingTranslator) Get(id string) (translation.Task, error) {
	translator.mu.RLock()
	defer translator.mu.RUnlock()
	if translator.task.ID != id {
		return translation.Task{}, translation.ErrTaskNotFound
	}
	return translator.task, nil
}

type checkpointRebuilder struct {
	calls atomic.Int32
	task  publisher.Task
}

type blockingRebuilder struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
	calls       atomic.Int32
}

func newBlockingRebuilder() *blockingRebuilder {
	return &blockingRebuilder{started: make(chan struct{}), release: make(chan struct{})}
}

func (rebuilder *blockingRebuilder) Build(context.Context) (publisher.BuildReport, error) {
	rebuilder.calls.Add(1)
	rebuilder.startedOnce.Do(func() { close(rebuilder.started) })
	<-rebuilder.release
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
}

func (rebuilder *blockingRebuilder) unblock() {
	rebuilder.releaseOnce.Do(func() { close(rebuilder.release) })
}

func (rebuilder *checkpointRebuilder) Build(context.Context) (publisher.BuildReport, error) {
	rebuilder.calls.Add(1)
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
}

func (rebuilder *checkpointRebuilder) GetTask(id string) (publisher.Task, error) {
	if rebuilder.task.ID == id {
		return rebuilder.task, nil
	}
	return publisher.Task{}, publisher.ErrBuildTaskNotFound
}

func TestScheduledPublishRunsPostAndPageOnceAndCompletesAtOneHundred(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, contentService, item := scheduledFixture(t, kind)
			dueAt := time.Now().UTC().Add(150 * time.Millisecond)
			item = configureSchedule(t, contentService, kind, item, dueAt)
			rebuilder := &countingRebuilder{}
			service := NewService(repository, contentService, rebuilder, nil, nil)
			defer service.Close()
			task, err := service.Start(StartInput{EntityKind: kind, EntityID: item.Meta.ID, Revision: item.Meta.Revision, DueAt: dueAt})
			if err != nil {
				t.Fatal(err)
			}
			task = waitForTerminal(t, service, task.ID)
			if task.Status != "succeeded" || task.Progress.Percent != 100 || task.Progress.Current != task.Progress.Total || task.Outcome != "published" || rebuilder.calls.Load() != 1 {
				t.Fatalf("task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
			}
			published, err := scheduledContent(contentService, kind, item.Meta.ID)
			if err != nil || published.Meta.Status != domain.ContentStatusPublished || published.Meta.ReleaseRevision == 0 || published.Meta.ScheduledRevision != 0 {
				t.Fatalf("published content = %#v, %v", published, err)
			}
		})
	}
}

func TestCloseAndLaunchAreOrderedAndClosedServiceKeepsNoTimer(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		service := NewService(nil, nil, nil, nil, nil)
		task := Task{ID: "scheduled-publish-20260811T010203.000000000Z-aabbccdd", DueAt: time.Now().UTC().Add(time.Hour)}
		start := make(chan struct{})
		launchDone := make(chan struct{})
		closeDone := make(chan struct{})
		go func() {
			<-start
			_ = service.launch(task)
			close(launchDone)
		}()
		go func() {
			<-start
			service.Close()
			close(closeDone)
		}()
		close(start)
		select {
		case <-launchDone:
		case <-time.After(time.Second):
			t.Fatal("launch did not return while Close was running")
		}
		select {
		case <-closeDone:
		case <-time.After(time.Second):
			t.Fatal("Close did not wait for the concurrently launched timer")
		}
		service.timersMu.Lock()
		remainingTimers := len(service.timers)
		service.timersMu.Unlock()
		if remainingTimers != 0 || service.launch(task) {
			t.Fatalf("closed service retained or accepted a timer: remaining=%d", remainingTimers)
		}
	}
}

func TestCheckpointFailureAfterPublicationStillRunsChildrenAndPersistsTerminalState(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(200 * time.Millisecond)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	rebuilder := &countingRebuilder{}
	translator := &countingTranslator{}
	service := NewService(repository, contentService, rebuilder, translator, nil)
	defer service.Close()
	var committedCheckpointFailures atomic.Int32
	var terminalAttempts atomic.Int32
	service.writeTaskHook = func(candidate Task) error {
		if candidate.Status == "running" && candidate.Progress.Phase == "scheduled-translation" && candidate.TranslationTaskID != "" && candidate.BuildStatus == "deferred" {
			if committedCheckpointFailures.Add(1) <= taskCheckpointWriteAttempts {
				return errors.New("injected post-publication checkpoint failure")
			}
		}
		if candidate.Status == "succeeded" {
			if terminalAttempts.Add(1) <= taskCheckpointWriteAttempts {
				return errors.New("injected terminal checkpoint failure")
			}
		}
		return nil
	}
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}

	task = waitForTerminal(t, service, task.ID)
	if task.Status != "succeeded" || task.Progress.Percent != 100 || task.BuildStatus != "deferred" || task.BuildTaskID != "" || task.TranslationStatus != "queued" || task.TranslationTaskID == "" || rebuilder.calls.Load() != 0 || translator.prepareCalls.Load() != 1 || translator.launchCalls.Load() != 1 || committedCheckpointFailures.Load() != taskCheckpointWriteAttempts || terminalAttempts.Load() != taskCheckpointWriteAttempts+1 {
		t.Fatalf("task = %#v, rebuilds = %d, translation prepares = %d, launches = %d, committed checkpoint failures = %d, terminal attempts = %d", task, rebuilder.calls.Load(), translator.prepareCalls.Load(), translator.launchCalls.Load(), committedCheckpointFailures.Load(), terminalAttempts.Load())
	}
	published, err := contentService.GetPost(post.Meta.ID)
	if err != nil || published.Meta.Status != domain.ContentStatusPublished || published.Meta.ReleaseRevision == 0 {
		t.Fatalf("published content = %#v, %v", published.Meta, err)
	}
}

func TestCompletedParentReflectsTranslationChildWithoutWaitingOrRewriting(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	translator := translation.NewService(repository, nil, nil, nil)
	defer translator.Close()
	service := NewService(repository, nil, nil, translator, nil)
	defer service.Close()
	now := time.Now().UTC()
	task := Task{
		SchemaVersion: domain.SchemaVersion, ID: "scheduled-publish-20260811T010203.000000000Z-aabbccdd", Kind: "ScheduledPublish", Operation: "publish",
		EntityKind: "Post", EntityID: "scheduled-post", Revision: 3, DueAt: now.Add(time.Hour), FirstPublish: true,
		Status: "succeeded", Progress: taskstore.Progress{Phase: "completed", Current: 4, Total: 4, Percent: 100}, BuildStatus: "succeeded",
		TranslationStatus: "queued", TranslationTaskID: "translation-scheduled-test", Outcome: "published", CreatedAt: now,
	}
	if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		childStatus string
		expected    string
		outcome     string
	}{
		{childStatus: "queued", expected: "queued", outcome: "published"},
		{childStatus: "running", expected: "running", outcome: "published"},
		{childStatus: "succeeded", expected: "succeeded", outcome: "published"},
		{childStatus: "failed", expected: "failed", outcome: "published-with-warning"},
		{childStatus: "needs-review", expected: "needs-review", outcome: "published-with-warning"},
		{childStatus: "unknown", expected: "failed", outcome: "published-with-warning"},
	} {
		child := translation.Task{SchemaVersion: domain.SchemaVersion, ID: task.TranslationTaskID, Kind: "Translation", Status: test.childStatus, CreatedAt: now}
		if err := repository.WriteYAML(filepath.Join("state", "tasks", child.ID+".yaml"), child, false); err != nil {
			t.Fatal(err)
		}
		observed, getErr := service.Get(task.ID)
		if getErr != nil || observed.Status != "succeeded" || observed.TranslationStatus != test.expected || observed.Outcome != test.outcome {
			t.Fatalf("Get() for child %q = %#v, %v", test.childStatus, observed, getErr)
		}
		tasks, listErr := service.List()
		if listErr != nil || len(tasks) != 1 || tasks[0].TranslationStatus != test.expected || tasks[0].Outcome != test.outcome {
			t.Fatalf("List() for child %q = %#v, %v", test.childStatus, tasks, listErr)
		}
	}

	if err := repository.RemoveFile(filepath.Join("state", "tasks", task.TranslationTaskID+".yaml")); err != nil {
		t.Fatal(err)
	}
	missing, err := service.Get(task.ID)
	if err != nil || missing.TranslationStatus != "failed" || missing.Outcome != "published-with-warning" {
		t.Fatalf("Get() with missing durable translation child = %#v, %v", missing, err)
	}

	var stored Task
	if err := repository.ReadYAML(filepath.Join("state", "tasks", task.ID+".yaml"), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.TranslationStatus != "queued" || stored.Outcome != "published" {
		t.Fatalf("read-time child projection rewrote stored parent = %#v", stored)
	}

	child := translation.Task{SchemaVersion: domain.SchemaVersion, ID: task.TranslationTaskID, Kind: "Translation", Status: "failed", CreatedAt: now}
	if err := repository.WriteYAML(filepath.Join("state", "tasks", child.ID+".yaml"), child, false); err != nil {
		t.Fatal(err)
	}
	task.Status = "running"
	task.Outcome = ""
	if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
		t.Fatal(err)
	}
	running, err := service.Get(task.ID)
	if err != nil || running.TranslationStatus != "queued" || running.Outcome != "" {
		t.Fatalf("running parent was hydrated = %#v, %v", running, err)
	}
	task.Status = "succeeded"
	task.Outcome = "superseded"
	if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
		t.Fatal(err)
	}
	superseded, err := service.Get(task.ID)
	if err != nil || superseded.TranslationStatus != "queued" || superseded.Outcome != "superseded" {
		t.Fatalf("superseded parent was hydrated = %#v, %v", superseded, err)
	}

	task.Outcome = "published"
	if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
		t.Fatal(err)
	}
	serviceWithoutReader := NewService(repository, nil, nil, nil, nil)
	defer serviceWithoutReader.Close()
	opaque, err := serviceWithoutReader.Get(task.ID)
	if err != nil || opaque.TranslationStatus != "queued" || opaque.Outcome != "published" {
		t.Fatalf("parent changed without a translation reader = %#v, %v", opaque, err)
	}
}

func TestRunReleasesMutationGateBeforeBlockedBuildAndNeverReacquires(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	rebuilder := newBlockingRebuilder()
	defer rebuilder.unblock()
	var mutationGate sync.RWMutex
	service := NewService(repository, contentService, rebuilder, nil, func() func() {
		mutationGate.Lock()
		return mutationGate.Unlock
	})
	defer service.Close()
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	service.cancelTimer(task.ID)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		service.run(context.Background(), task.ID)
	}()
	defer func() {
		rebuilder.unblock()
		select {
		case <-runDone:
		case <-time.After(3 * time.Second):
			t.Error("direct scheduled runner did not exit during test cleanup")
		}
	}()
	select {
	case <-rebuilder.started:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled build did not start")
	}
	acquired := make(chan struct{})
	go func() {
		mutationGate.RLock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(3 * time.Second):
		t.Fatal("admin reader could not acquire mutation gate during scheduled build")
	}
	defer mutationGate.RUnlock()
	rebuilder.unblock()
	task = waitForTerminal(t, service, task.ID)
	if task.Status != "succeeded" || rebuilder.calls.Load() != 1 {
		t.Fatalf("task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
	}
}

func TestRunKeepsMutationGateThroughReceiptThenDefersBuildToTranslation(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	rebuilder := &countingRebuilder{}
	translator := &countingTranslator{}
	var mutationGate sync.RWMutex
	service := NewService(repository, contentService, rebuilder, translator, func() func() {
		mutationGate.Lock()
		return mutationGate.Unlock
	})
	defer service.Close()
	checkpointEntered := make(chan struct{})
	releaseCheckpoint := make(chan struct{})
	var checkpointOnce sync.Once
	var releaseCheckpointOnce sync.Once
	releaseChildReceiptCheckpoint := func() {
		releaseCheckpointOnce.Do(func() { close(releaseCheckpoint) })
	}
	defer releaseChildReceiptCheckpoint()
	service.writeTaskHook = func(candidate Task) error {
		if candidate.Status == "running" && candidate.Progress.Phase == "scheduled-translation" && candidate.TranslationTaskID != "" && candidate.BuildStatus == "deferred" && candidate.BuildTaskID == "" {
			checkpointOnce.Do(func() {
				close(checkpointEntered)
				<-releaseCheckpoint
			})
		}
		return nil
	}
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	service.cancelTimer(task.ID)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		service.run(context.Background(), task.ID)
	}()
	defer func() {
		releaseChildReceiptCheckpoint()
		select {
		case <-runDone:
		case <-time.After(3 * time.Second):
			t.Error("direct scheduled runner did not exit during test cleanup")
		}
	}()
	select {
	case <-checkpointEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("post-commit child receipt checkpoint was not reached")
	}
	if mutationGate.TryRLock() {
		mutationGate.RUnlock()
		t.Fatal("mutation gate was released before child receipts were durably checkpointed")
	}
	if translator.prepareCalls.Load() != 1 || translator.launchCalls.Load() != 0 {
		t.Fatalf("translation prepares = %d, launches = %d before receipt checkpoint", translator.prepareCalls.Load(), translator.launchCalls.Load())
	}
	releaseChildReceiptCheckpoint()
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled runner did not finish after child receipt checkpoint")
	}
	if !mutationGate.TryRLock() {
		t.Fatal("mutation gate remained exclusive after translation receipt checkpoint")
	}
	mutationGate.RUnlock()
	task, err = service.Get(task.ID)
	if err != nil || task.Status != "succeeded" || task.BuildStatus != "deferred" || task.BuildTaskID != "" || rebuilder.calls.Load() != 0 || translator.launchCalls.Load() != 1 {
		t.Fatalf("task = %#v, err = %v, builds = %d, translation launches = %d", task, err, rebuilder.calls.Load(), translator.launchCalls.Load())
	}
	translator.mu.RLock()
	translationInput := translator.input
	translator.mu.RUnlock()
	if !translationInput.PublishedRelease || !translationInput.SkipCurrentAI || !translationInput.OverwriteManual || !translationInput.RecordPreflightFailure {
		t.Fatalf("automatic scheduled translation input = %#v", translationInput)
	}
}

func TestInvalidateForEntityUsesLatestRevisionAfterDelayedCallback(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	firstDueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, firstDueAt)
	service := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer service.Close()
	first, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: firstDueAt})
	if err != nil {
		t.Fatal(err)
	}
	source := post.Content[post.Meta.SourceLocale]
	changed, err := contentService.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, content.UpdateLocaleInput{
		ExpectedRevision: post.Meta.Revision, Title: source.Title + " changed", Markdown: source.Markdown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := service.InvalidateForEntity("Post", post.Meta.ID, changed.Meta.Revision); err != nil || count != 1 {
		t.Fatalf("invalidate first schedule = %d, %v", count, err)
	}
	first, err = service.Get(first.ID)
	if err != nil || first.Status != "needs-review" {
		t.Fatalf("first schedule = %#v, %v", first, err)
	}
	secondDueAt := firstDueAt.Add(time.Hour)
	newer := configureSchedule(t, contentService, "Post", changed, secondDueAt)
	second, err := service.Start(StartInput{EntityKind: "Post", EntityID: newer.Meta.ID, Revision: newer.Meta.Revision, DueAt: secondDueAt})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := service.InvalidateForEntity("Post", newer.Meta.ID, changed.Meta.Revision); err != nil || count != 0 {
		t.Fatalf("delayed invalidation = %d, %v", count, err)
	}
	second, err = service.Get(second.ID)
	if err != nil || second.Status != "queued" {
		t.Fatalf("new schedule was invalidated by stale callback: %#v, %v", second, err)
	}
	current, err := contentService.GetPost(newer.Meta.ID)
	if err != nil || current.Meta.ScheduledRevision != newer.Meta.Revision {
		t.Fatalf("new scheduled intent = %#v, %v", current.Meta, err)
	}
}

func TestInvalidateForEntityPreservesCommittedPublicationAwaitingRecoveryBuild(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	service := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer service.Close()
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	service.cancelTimer(task.ID)
	published, err := contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	source := published.Content[published.Meta.SourceLocale]
	advanced, err := contentService.UpdateLocale(published.Meta.ID, published.Meta.SourceLocale, content.UpdateLocaleInput{
		ExpectedRevision: published.Meta.Revision, Title: source.Title + " AI", Markdown: source.Markdown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := service.InvalidateForEntity("Post", advanced.Meta.ID, advanced.Meta.Revision); err != nil || count != 0 {
		t.Fatalf("committed publication invalidation = %d, %v", count, err)
	}
	stored, err := service.Get(task.ID)
	if err != nil || stored.Status != "queued" {
		t.Fatalf("committed parent was terminalized before recovery build: %#v, %v", stored, err)
	}
}

func TestImmediatePublishSupersedesQueuedScheduleWithoutLaterRebuild(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Minute)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	rebuilder := &countingRebuilder{}
	service := NewService(repository, contentService, rebuilder, nil, nil)
	defer service.Close()
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentService.PublishPost(post.Meta.ID, post.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	if count, err := service.CancelForEntity("Post", post.Meta.ID); err != nil || count != 1 {
		t.Fatalf("CancelForEntity() = %d, %v", count, err)
	}
	time.Sleep(20 * time.Millisecond)
	task, err = service.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "succeeded" || task.Outcome != "superseded" || task.Progress.Percent != 100 || rebuilder.calls.Load() != 0 {
		t.Fatalf("superseded task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
	}
}

func TestChangedContentImmediatelyMovesScheduleToNeedsReview(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	service := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer service.Close()
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	source := post.Content[post.Meta.SourceLocale]
	changed, err := contentService.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, content.UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: source.Title + " changed", Markdown: source.Markdown})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := service.InvalidateForEntity("Post", post.Meta.ID, changed.Meta.Revision); err != nil || count != 1 {
		t.Fatalf("InvalidateForEntity() = %d, %v", count, err)
	}
	task, err = service.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "needs-review" || task.Progress.Percent != 0 || task.Error != "content-changed-before-scheduled-publish" {
		t.Fatalf("invalidated task = %#v", task)
	}
	changed, err = contentService.GetPost(post.Meta.ID)
	if err != nil || changed.Meta.ScheduledRevision != 0 {
		t.Fatalf("scheduled intent after invalidation = %#v, %v", changed.Meta, err)
	}
	if _, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt}); !errors.Is(err, content.ErrConflict) {
		t.Fatalf("stale Start() error = %v, want content.ErrConflict", err)
	}
}

func TestIdempotentStartAndReschedule(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	firstDueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, firstDueAt)
	service := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer service.Close()
	first, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: firstDueAt})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: firstDueAt})
	if err != nil || repeated.ID != first.ID {
		t.Fatalf("idempotent Start() = %#v, %v", repeated, err)
	}
	secondDueAt := firstDueAt.Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, secondDueAt)
	if _, err := service.InvalidateForEntity("Post", post.Meta.ID, post.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	second, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: secondDueAt})
	if err != nil || second.ID == first.ID {
		t.Fatalf("rescheduled Start() = %#v, %v", second, err)
	}
	old, err := service.Get(first.ID)
	if err != nil || old.Status != "needs-review" {
		t.Fatalf("old schedule = %#v, %v", old, err)
	}
}

func TestRescheduleDoesNotCancelAnAlreadyRunningTask(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	firstDueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, firstDueAt)
	service := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer service.Close()
	running, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: firstDueAt})
	if err != nil {
		t.Fatal(err)
	}
	running.Status = "running"
	started := time.Now().UTC()
	running.StartedAt = &started
	if err := service.writeTask(running); err != nil {
		t.Fatal(err)
	}
	secondDueAt := firstDueAt.Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, secondDueAt)
	if _, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: secondDueAt}); !errors.Is(err, ErrTaskRunning) {
		t.Fatalf("reschedule error = %v, want ErrTaskRunning", err)
	}
	stored, err := service.Get(running.ID)
	if err != nil || stored.Status != "running" || stored.CompletedAt != nil {
		t.Fatalf("running task was changed = %#v, %v", stored, err)
	}
}

func TestRecoverRecreatesMissingTaskFromBackedUpContentIntent(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	firstService := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	first, err := firstService.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	firstService.Close()
	if err := repository.RemoveFile("state/tasks/" + first.ID + ".yaml"); err != nil {
		t.Fatal(err)
	}
	recoveredService := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer recoveredService.Close()
	if recovered, err := recoveredService.Recover(); err != nil || recovered != 1 {
		t.Fatalf("Recover() = %d, %v", recovered, err)
	}
	tasks, err := recoveredService.List()
	if err != nil || len(tasks) != 1 || tasks[0].ID == first.ID || tasks[0].Status != "queued" || tasks[0].Revision != post.Meta.Revision {
		t.Fatalf("recreated tasks = %#v, %v", tasks, err)
	}
}

func TestLiveReconcileReplacesCanceledTimerForSameTask(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(time.Hour)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	service := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	defer service.Close()
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	service.timersMu.Lock()
	before := service.timers[task.ID]
	service.timersMu.Unlock()
	if recovered, err := service.Reconcile(); err != nil || recovered != 1 {
		t.Fatalf("Reconcile() = %d, %v", recovered, err)
	}
	service.timersMu.Lock()
	after := service.timers[task.ID]
	service.timersMu.Unlock()
	if before == nil || after == nil || before == after {
		t.Fatalf("timer was not replaced: before=%p after=%p", before, after)
	}
}

func TestRecoverResumesBuildAfterPublishWasPersisted(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(250 * time.Millisecond)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	firstService := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	task, err := firstService.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	firstService.Close()
	if _, err := contentService.PublishPost(post.Meta.ID, post.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	rebuilder := &countingRebuilder{}
	recoveredService := NewService(repository, contentService, rebuilder, nil, nil)
	defer recoveredService.Close()
	if recovered, err := recoveredService.Recover(); err != nil || recovered != 1 {
		t.Fatalf("Recover() = %d, %v", recovered, err)
	}
	task = waitForTerminal(t, recoveredService, task.ID)
	if task.Status != "succeeded" || rebuilder.calls.Load() != 1 {
		t.Fatalf("recovered task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
	}
}

func TestRecoverCompletesHeadCommittedBeforeReleasePointerForPostAndPage(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, contentService, item := scheduledFixture(t, kind)
			var err error
			if kind == "Page" {
				item, err = contentService.PublishPage(item.Meta.ID, item.Meta.Revision)
			} else {
				item, err = contentService.PublishPost(item.Meta.ID, item.Meta.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			oldReleaseRevision := item.Meta.Revision
			dueAt := time.Now().UTC().Add(300 * time.Millisecond)
			item = configureSchedule(t, contentService, kind, item, dueAt)
			scheduledRevision := item.Meta.Revision
			firstService := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
			task, err := firstService.Start(StartInput{EntityKind: kind, EntityID: item.Meta.ID, Revision: scheduledRevision, DueAt: dueAt})
			if err != nil {
				t.Fatal(err)
			}
			firstService.Close()

			// Reproduce a process stop after PublishPost/Page committed the head
			// (including clearing the intent) but before writeRelease advanced the
			// immutable public pointer.
			directory := "posts"
			if kind == "Page" {
				directory = "pages"
			}
			metaPath := filepath.Join("content", directory, item.Meta.ID, "meta.yaml")
			var committed domain.PostMeta
			if err := repository.ReadYAML(metaPath, &committed); err != nil {
				t.Fatal(err)
			}
			committed.Status = domain.ContentStatusPublished
			committed.ScheduledRevision = 0
			committed.Revision++
			committed.HeadRevision = committed.Revision
			committed.ReleaseRevision = committed.Revision
			committed.UpdatedAt = time.Now().UTC()
			if err := repository.WriteYAML(metaPath, committed, false); err != nil {
				t.Fatal(err)
			}
			if _, err := contentService.InitializePublishedReleases(); err != nil {
				t.Fatal(err)
			}
			staleRelease, err := contentService.GetPublishedRelease(kind, item.Meta.ID)
			if err != nil || staleRelease.Meta.Revision != oldReleaseRevision {
				t.Fatalf("pre-recovery release = %#v, %v; want old revision %d", staleRelease.Meta, err, oldReleaseRevision)
			}
			time.Sleep(time.Until(dueAt) + 25*time.Millisecond)

			rebuilder := &countingRebuilder{}
			recoveredService := NewService(repository, contentService, rebuilder, nil, nil)
			defer recoveredService.Close()
			if recovered, err := recoveredService.Recover(); err != nil || recovered != 1 {
				t.Fatalf("Recover() = %d, %v", recovered, err)
			}
			task = waitForTerminal(t, recoveredService, task.ID)
			head, err := scheduledContent(contentService, kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			released, err := contentService.GetPublishedRelease(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != "succeeded" || task.Progress.Percent != 100 || rebuilder.calls.Load() != 1 || head.Meta.Revision != scheduledRevision+1 || head.Meta.ReleaseRevision != head.Meta.Revision || released.Meta.Revision != head.Meta.Revision {
				t.Fatalf("recovered task=%#v head=%#v release=%#v rebuilds=%d", task, head.Meta, released.Meta, rebuilder.calls.Load())
			}
		})
	}
}

func TestRecoverFinalizesPersistedBuildAndTranslationCheckpointsWithoutRepeatingEffects(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(250 * time.Millisecond)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	firstService := NewService(repository, contentService, &countingRebuilder{}, nil, nil)
	task, err := firstService.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	firstService.Close()
	published, err := contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a recovered translation having already advanced the content head
	// after the scheduled publish checkpoint. The release still proves that the
	// scheduled revision was published, so recovery must not repeat child work.
	source := published.Content[published.Meta.SourceLocale]
	if _, err := contentService.UpdateLocale(published.Meta.ID, published.Meta.SourceLocale, content.UpdateLocaleInput{
		ExpectedRevision: published.Meta.Revision, Title: source.Title + " translated checkpoint", Markdown: source.Markdown,
	}); err != nil {
		t.Fatal(err)
	}
	task.Status = "running"
	started := time.Now().UTC()
	task.StartedAt = &started
	task.Progress = taskstore.Progress{Phase: "scheduled-finalize", Current: 3, Total: 4, Percent: 95, Message: "finalizing-scheduled-publish"}
	task.BuildTaskID = "20260811T010203.000000000Z-aabbccdd"
	task.TranslationStatus = "queued"
	task.TranslationTaskID = "translation-20260811T010203.000000000Z-aabbccdd"
	if err := firstService.writeTask(task); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	rebuilder := &checkpointRebuilder{task: publisher.Task{SchemaVersion: domain.SchemaVersion, ID: task.BuildTaskID, Kind: "StaticBuild", Status: "succeeded"}}
	translator := &countingTranslator{}
	recoveredService := NewService(repository, contentService, rebuilder, translator, nil)
	defer recoveredService.Close()
	if recovered, err := recoveredService.Recover(); err != nil || recovered != 1 {
		t.Fatalf("Recover() = %d, %v", recovered, err)
	}
	task = waitForTerminal(t, recoveredService, task.ID)
	if task.Status != "succeeded" || task.Progress.Percent != 100 || task.BuildStatus != "deferred" || task.BuildTaskID != "" || task.TranslationTaskID == "" || rebuilder.calls.Load() != 0 || translator.prepareCalls.Load() != 0 || translator.launchCalls.Load() != 0 {
		t.Fatalf("checkpoint task = %#v, rebuilds = %d, translation prepares = %d, launches = %d", task, rebuilder.calls.Load(), translator.prepareCalls.Load(), translator.launchCalls.Load())
	}
}

func TestPublishedContentIsRecoveryCheckpointWithoutTaskFileProgress(t *testing.T) {
	dueAt := time.Now().UTC().Truncate(time.Second)
	task := Task{Revision: 7, DueAt: dueAt, Progress: taskstore.Progress{Phase: "scheduled-publish", Percent: 10}}
	publishedAt := dueAt
	item := domain.Post{Meta: domain.PostMeta{
		Status:            domain.ContentStatusPublished,
		Revision:          10,
		ReleaseRevision:   10,
		ScheduledRevision: 0,
		PublishedAt:       &publishedAt,
	}}
	if !publishedByScheduledTask(task, item) {
		t.Fatal("durable publication was not recognized without a post-publish task checkpoint")
	}
	item.Meta.ScheduledRevision = task.Revision
	if publishedByScheduledTask(task, item) {
		t.Fatal("uncleared schedule was mistaken for a durable publication")
	}
}

func TestScheduledTranslationOwnsBuildAndCompletesParentWithoutSourceBuild(t *testing.T) {
	repository, contentService, post := scheduledFixture(t, "Post")
	dueAt := time.Now().UTC().Add(150 * time.Millisecond)
	post = configureSchedule(t, contentService, "Post", post, dueAt)
	rebuilder := &countingRebuilder{err: errors.New("renderer failed")}
	translator := &countingTranslator{}
	service := NewService(repository, contentService, rebuilder, translator, nil)
	defer service.Close()
	task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	task = waitForTerminal(t, service, task.ID)
	if task.Status != "succeeded" || task.Error != "" || task.BuildStatus != "deferred" || task.BuildTaskID != "" || task.Outcome != "published" || task.Progress.Percent != 100 || rebuilder.calls.Load() != 0 || translator.prepareCalls.Load() != 1 || translator.launchCalls.Load() != 1 || task.TranslationTaskID == "" {
		t.Fatalf("published task = %#v, builds = %d, translation prepares = %d, launches = %d", task, rebuilder.calls.Load(), translator.prepareCalls.Load(), translator.launchCalls.Load())
	}
}

func TestTranslationPreparationWarningsCompletePublishedParentWithExplicitOutcome(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status string
	}{
		{name: "queue-failed", err: errors.New("translation queue unavailable"), status: "failed"},
		{name: "provider-not-configured", err: ai.ErrKeyMissing, status: "not-configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, contentService, post := scheduledFixture(t, "Post")
			dueAt := time.Now().UTC().Add(time.Hour)
			post = configureSchedule(t, contentService, "Post", post, dueAt)
			translator := &countingTranslator{err: test.err}
			rebuilder := &countingRebuilder{}
			service := NewService(repository, contentService, rebuilder, translator, nil)
			defer service.Close()
			task, err := service.Start(StartInput{EntityKind: "Post", EntityID: post.Meta.ID, Revision: post.Meta.Revision, DueAt: dueAt})
			if err != nil {
				t.Fatal(err)
			}
			service.cancelTimer(task.ID)
			service.run(context.Background(), task.ID)
			task, err = service.Get(task.ID)
			if err != nil || task.Status != "succeeded" || task.Error != "" || task.Progress.Percent != 100 || task.BuildStatus != "blocked" || task.BuildTaskID != "" || task.TranslationStatus != test.status || task.Outcome != "published-with-warning" || rebuilder.calls.Load() != 0 || translator.prepareCalls.Load() != 1 || translator.launchCalls.Load() != 0 {
				t.Fatalf("published task with translation warning = %#v, %v, builds = %d, translation prepares = %d, launches = %d", task, err, rebuilder.calls.Load(), translator.prepareCalls.Load(), translator.launchCalls.Load())
			}
			var stored Task
			if err := repository.ReadYAML(filepath.Join("state", "tasks", task.ID+".yaml"), &stored); err != nil || stored.TranslationStatus != test.status || stored.Outcome != "published-with-warning" {
				t.Fatalf("stored published task warning = %#v, %v", stored, err)
			}
		})
	}
}

func TestListSkipsValidForeignKindsButRejectsUnknownHeader(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil, nil)
	defer service.Close()
	foreign := []struct {
		id   string
		kind string
	}{
		{id: "translation-example", kind: "Translation"},
		{id: "backup-task-example", kind: "Backup"},
		{id: "20260811T010203.000000000Z-aabbccdd", kind: "StaticBuild"},
		{id: "index-rebuild-20260811T010203.000000000Z-aabbccdd", kind: "IndexRebuild"},
	}
	for _, task := range foreign {
		if err := repository.WriteYAML("state/tasks/"+task.id+".yaml", map[string]any{
			"schemaVersion": domain.SchemaVersion, "id": task.id, "kind": task.kind,
		}, false); err != nil {
			t.Fatal(err)
		}
	}
	if tasks, err := service.List(); err != nil || len(tasks) != 0 {
		t.Fatalf("scheduled tasks = %#v, %v", tasks, err)
	}
	if err := repository.WriteYAML("state/tasks/unknown.yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion, "id": "unknown", "kind": "Unknown",
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(); err == nil {
		t.Fatal("expected unknown shared task kind to be reported")
	}
}

func waitForTerminal(t *testing.T, service *Service, id string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := service.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status != "queued" && task.Status != "running" {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := service.Get(id)
	t.Fatalf("task did not finish: %#v", task)
	return Task{}
}

func scheduledFixture(t *testing.T, kind string) (*fsrepo.Repository, *content.Service, domain.Post) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN",
		Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}}, Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	var item domain.Post
	if kind == "Page" {
		item, err = contentService.CreatePage(content.CreatePageInput{ID: "scheduled-page", Title: "计划页面", Markdown: "正文"})
	} else {
		item, err = contentService.CreatePost(content.CreatePostInput{ID: "scheduled-post", Title: "计划发布", Markdown: "正文"})
	}
	if err != nil {
		t.Fatal(err)
	}
	return repository, contentService, item
}

func configureSchedule(t *testing.T, service *content.Service, kind string, item domain.Post, dueAt time.Time) domain.Post {
	t.Helper()
	visibility := item.Meta.Visibility
	pinned := item.Meta.Pinned
	input := content.UpdatePostSettingsInput{
		ExpectedRevision: item.Meta.Revision, Categories: item.Meta.Categories, Tags: item.Meta.Tags, Cover: item.Meta.Cover,
		Pinned: &pinned, Visibility: &visibility, PublishedAt: &dueAt, PublishTimeSet: true,
		CommentPolicy: item.Meta.CommentPolicy, Template: item.Meta.Template,
	}
	var updated domain.Post
	var err error
	if kind == "Page" {
		updated, err = service.UpdatePageSettings(item.Meta.ID, input)
	} else {
		updated, err = service.UpdatePostSettings(item.Meta.ID, input)
	}
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func scheduledContent(service *content.Service, kind, id string) (domain.Post, error) {
	if kind == "Page" {
		return service.GetPage(id)
	}
	return service.GetPost(id)
}
