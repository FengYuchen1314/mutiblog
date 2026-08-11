package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

type backupTaskStarterFunc func(string) (backup.Task, error)

func (fn backupTaskStarterFunc) StartTask(id string) (backup.Task, error) {
	return fn(id)
}

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
	startAttempts := 0
	firstAttempt := make(chan struct{})
	releaseFirstAttempt := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseFirstAttempt) })
	server := &Server{
		backups: backups,
		backupTaskStarter: backupTaskStarterFunc(func(string) (backup.Task, error) {
			startAttempts++
			if startAttempts == 1 {
				close(firstAttempt)
				<-releaseFirstAttempt
			}
			return backup.Task{}, errors.New("injected start checkpoint failure")
		}),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan struct{})
	go func() {
		server.runBackupCreate(context.Background(), task.ID)
		close(done)
	}()

	select {
	case <-firstAttempt:
	case <-time.After(2 * time.Second):
		t.Fatal("backup runner did not reach its start checkpoint")
	}
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
	releaseOnce.Do(func() { close(releaseFirstAttempt) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("backup start checkpoint failure did not finish within its bounded retry window")
	}
	if startAttempts != 3 {
		t.Fatalf("start checkpoint attempts = %d, want 3", startAttempts)
	}
	stored, err := backups.GetTask(task.ID)
	if err != nil || stored.Status != "failed" || stored.Error != "progress-write-failed" {
		t.Fatalf("failed backup task = %#v, %v", stored, err)
	}
}
