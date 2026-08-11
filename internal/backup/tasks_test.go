package backup

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestBackupTaskCheckpointRetryAndBackgroundTerminalPersistence(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	defer service.Close()
	task, err := service.CreateTask("create")
	if err != nil {
		t.Fatal(err)
	}
	var startAttempts atomic.Int32
	var terminalAttempts atomic.Int32
	service.writeTaskHook = func(candidate Task) error {
		if candidate.Status == "running" && candidate.Progress.Percent == 5 {
			if startAttempts.Add(1) < taskCheckpointWriteAttempts {
				return errors.New("injected start checkpoint failure")
			}
		}
		if candidate.Status == "succeeded" {
			if terminalAttempts.Add(1) <= taskCheckpointWriteAttempts {
				return errors.New("injected terminal checkpoint failure")
			}
		}
		return nil
	}
	if _, err := service.StartTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FinishTask(task.ID, "succeeded", "backup-example", ""); err == nil {
		t.Fatal("expected synchronous terminal checkpoint attempts to fail")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stored, getErr := service.GetTask(task.ID)
		if getErr == nil && stored.Status == "succeeded" {
			if startAttempts.Load() != taskCheckpointWriteAttempts || terminalAttempts.Load() != taskCheckpointWriteAttempts+1 || stored.BackupID != "backup-example" {
				t.Fatalf("stored=%#v start attempts=%d terminal attempts=%d", stored, startAttempts.Load(), terminalAttempts.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("terminal backup checkpoint was not persisted in the live service")
}

func TestNewerBackupTerminalCannotBeOverwrittenByOlderRetry(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	defer service.Close()
	task, err := service.CreateTask("create")
	if err != nil {
		t.Fatal(err)
	}
	var oldAttempts atomic.Int32
	service.writeTaskHook = func(candidate Task) error {
		if candidate.Status == "failed" && oldAttempts.Add(1) <= taskCheckpointWriteAttempts {
			return errors.New("injected old terminal failure")
		}
		return nil
	}
	if _, err := service.FinishTask(task.ID, "failed", "", "old-outcome"); err == nil {
		t.Fatal("expected old terminal persistence to enter background retry")
	}
	if _, err := service.FinishTask(task.ID, "succeeded", "backup-newer", ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	stored, err := service.GetTask(task.ID)
	if err != nil || stored.Status != "succeeded" || stored.BackupID != "backup-newer" || stored.Error != "" {
		t.Fatalf("newer terminal was overwritten: %#v, %v", stored, err)
	}
}

func TestRecoveredCreateTaskReusesDeterministicBusinessReceipt(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	defer service.Close()
	task, err := service.CreateTask("create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartTask(task.ID); err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateForTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateForTask(task.ID)
	if err != nil || second.ID != first.ID {
		t.Fatalf("idempotent create receipt = %#v, %v; first = %#v", second, err, first)
	}
	retry, err := service.RecoverTasks()
	if err != nil || len(retry) != 0 {
		t.Fatalf("RecoverTasks() = %#v, %v", retry, err)
	}
	stored, err := service.GetTask(task.ID)
	if err != nil || stored.Status != "succeeded" || stored.BackupID != first.ID {
		t.Fatalf("recovered task = %#v, %v", stored, err)
	}
	records, err := service.List()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestBackupServiceCloseOrdersConcurrentLaunch(t *testing.T) {
	service := NewService(nil)
	started := make(chan struct{})
	finished := make(chan struct{})
	if !service.Launch(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("initial runner was rejected")
	}
	<-started
	service.Close()
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the managed backup runner")
	}
	if service.Launch(func(context.Context) {}) {
		t.Fatal("closed backup service accepted a runner")
	}
}

func TestBackupTaskLifecycleAndRecovery(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	completed, err := service.CreateTask("create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartTask(completed.ID); err != nil {
		t.Fatal(err)
	}
	if completed, err = service.FinishTask(completed.ID, "succeeded", "backup-example", ""); err != nil || completed.Status != "succeeded" || completed.Progress.Percent != 100 || completed.Progress.Current != completed.Progress.Total || completed.BackupID != "backup-example" {
		t.Fatalf("completed task = %#v, %v", completed, err)
	}
	retryCreate, err := service.CreateTask("create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartTask(retryCreate.ID); err != nil {
		t.Fatal(err)
	}
	retryLocal, err := service.CreateTask("import-local")
	if err != nil {
		t.Fatal(err)
	}
	localPath, _ := service.StagedImportPath(retryLocal.ID)
	if err := os.WriteFile(localPath, []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	nonReplayable, err := service.CreateTask("import-url")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 2 {
		t.Fatalf("recovered tasks = %#v", recovered)
	}
	failed, err := service.GetTask(nonReplayable.ID)
	if err != nil || failed.Status != "failed" || failed.Error == "" {
		t.Fatalf("non-replayable task = %#v, %v", failed, err)
	}
	tasks, err := service.ListTasks()
	if err != nil || len(tasks) != 4 {
		t.Fatalf("tasks = %#v, %v", tasks, err)
	}
}

func TestListTasksRejectsBackupTaskWhoseIdentityDoesNotMatchItsPath(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	if err := repository.WriteYAML("state/tasks/backup-task-expected.yaml", Task{
		SchemaVersion: 1,
		ID:            "backup-task-different",
		Kind:          "Backup",
		Operation:     "create",
		Status:        "queued",
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListTasks(); err == nil {
		t.Fatal("expected invalid backup task identity to be reported")
	}
}

func TestListTasksIgnoresValidTranslationAndStaticBuildTasks(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	foreign := []struct {
		id   string
		kind string
	}{
		{id: "translation-example", kind: "Translation"},
		{id: "20260811T010203.000000000Z-aabbccdd", kind: "StaticBuild"},
		{id: "scheduled-publish-20260811T010203.000000000Z-aabbccdd", kind: "ScheduledPublish"},
		{id: "index-rebuild-20260811T010203.000000000Z-aabbccdd", kind: "IndexRebuild"},
	}
	for _, task := range foreign {
		if err := repository.WriteYAML("state/tasks/"+task.id+".yaml", map[string]any{
			"schemaVersion": domain.SchemaVersion,
			"id":            task.id,
			"kind":          task.kind,
			"status":        "succeeded",
		}, false); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := service.ListTasks()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("backup tasks = %#v, %v", tasks, err)
	}
}

func TestListTasksRejectsUnknownSharedTaskKind(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	if err := repository.WriteYAML("state/tasks/unknown.yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion,
		"id":            "unknown",
		"kind":          "Unknown",
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListTasks(); err == nil {
		t.Fatal("expected an unknown shared task kind to be reported")
	}
}
