package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestBackupCreateStartCheckpointExhaustionDoesNotStarveMutationGate(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backups := backup.NewService(repository)
	defer backups.Close()
	task, err := backups.CreateTask("create")
	if err != nil {
		t.Fatal(err)
	}
	taskDirectory := filepath.Join(repository.Root(), "state", "tasks")
	if err := os.Chmod(taskDirectory, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(taskDirectory, 0o750)

	server := &Server{backups: backups, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	done := make(chan struct{})
	go func() {
		server.runBackupCreate(context.Background(), task.ID)
		close(done)
	}()

	gateAcquired := make(chan struct{})
	go func() {
		server.mutationGate.Lock()
		close(gateAcquired)
		server.mutationGate.Unlock()
	}()
	select {
	case <-gateAcquired:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("backup start checkpoint failure held the global mutation gate")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("backup start checkpoint failure did not finish within its bounded retry window")
	}
	if err := os.Chmod(taskDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stored, getErr := backups.GetTask(task.ID)
		if getErr == nil && stored.Status == "failed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("failed backup start did not persist a terminal state after storage recovered")
}
