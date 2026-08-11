package projection

import (
	"io"
	"log/slog"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

const testIndexRebuildTaskID = "index-rebuild-20260811T010203.000000000Z-aabbccdd"

func TestRebuildTaskPersistsProgressAndTerminalState(t *testing.T) {
	service := newTaskTestProjection(t)
	defer service.Close()

	task, err := service.CreateRebuildTask(testIndexRebuildTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "queued" || task.Progress.Total != 3 || task.Progress.Message != "waiting-to-rebuild-search-index" {
		t.Fatalf("queued task = %#v", task)
	}
	if _, err := service.StartRebuildTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateRebuildTaskProgress(task.ID, "index-rebuild", 1, 3, 25, "rebuilding-search-index"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FinishRebuildTask(task.ID, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "succeeded" || stored.Progress.Percent != 100 || stored.CompletedAt == nil || stored.Error != "" {
		t.Fatalf("terminal task = %#v", stored)
	}
	tasks, err := service.ListTasks()
	if err != nil || len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("ListTasks() = %#v, %v", tasks, err)
	}
}

func TestRecoverTasksMarksInterruptedRebuildFailed(t *testing.T) {
	service := newTaskTestProjection(t)
	defer service.Close()
	task, err := service.CreateRebuildTask(testIndexRebuildTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRebuildTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.Error != "interrupted" || stored.CompletedAt == nil {
		t.Fatalf("recovered task = %#v", stored)
	}
}

func newTaskTestProjection(t *testing.T) *Service {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := Open(repository, content.NewService(repository), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return service
}
