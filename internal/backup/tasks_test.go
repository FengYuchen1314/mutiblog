package backup

import (
	"os"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

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
	if completed, err = service.FinishTask(completed.ID, "succeeded", "backup-example", ""); err != nil || completed.Status != "succeeded" || completed.BackupID != "backup-example" {
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
