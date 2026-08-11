package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func restoreTestRepository(t *testing.T) *fsrepo.Repository {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: backup\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/locales.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nenabled:\n  - code: en\n    label: English\n    enabled: true\nfallback:\n  - en\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestRecoverInterruptedRestoreRollsBackPreparedSwap(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: current\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginRestore(record.ID); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	data, err := repository.ReadFile("config/site.yaml")
	if err != nil || !strings.Contains(string(data), "title: current") {
		t.Fatalf("recovered current site = %q, %v", data, err)
	}
	assertNoRestoreArtifacts(t, repository.Root())
}

func TestRecoverCommittedRestoreKeepsRestoredRoots(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: current\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	restoration, err := service.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoration.writeJournal("committed"); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	data, err := repository.ReadFile("config/site.yaml")
	if err != nil || !strings.Contains(string(data), "title: backup") {
		t.Fatalf("committed restored site = %q, %v", data, err)
	}
	assertNoRestoreArtifacts(t, repository.Root())
}

func TestRecoverPreparedRestoreAlsoRestoresPublicReleasePointer(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: current\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	previous := "releases/old-release"
	replacePublicReleaseForTest(t, repository.Root(), previous)
	if _, err := service.BeginRestoreWithPublicRelease(record.ID, previous); err != nil {
		t.Fatal(err)
	}
	replacePublicReleaseForTest(t, repository.Root(), "releases/candidate-release")

	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(repository.Root(), "generated", "current"))
	if err != nil || filepath.ToSlash(target) != previous {
		t.Fatalf("recovered public release = %q, %v", target, err)
	}
	data, err := repository.ReadFile("config/site.yaml")
	if err != nil || !strings.Contains(string(data), "title: current") {
		t.Fatalf("recovered current site = %q, %v", data, err)
	}
}

func TestRecoverCommittedRestorePreservesCandidatePublicRelease(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	previous := "releases/old-release"
	replacePublicReleaseForTest(t, repository.Root(), previous)
	restoration, err := service.BeginRestoreWithPublicRelease(record.ID, previous)
	if err != nil {
		t.Fatal(err)
	}
	candidate := "releases/candidate-release"
	replacePublicReleaseForTest(t, repository.Root(), candidate)
	if err := restoration.writeJournal("committed"); err != nil {
		t.Fatal(err)
	}

	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(repository.Root(), "generated", "current"))
	if err != nil || filepath.ToSlash(target) != candidate {
		t.Fatalf("committed public release = %q, %v", target, err)
	}
}

func TestRecoveryPreservesRootsWithoutRollbackCopy(t *testing.T) {
	repository := restoreTestRepository(t)
	if err := repository.WriteFile("content/posts/sentinel", []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	rollback := filepath.Join(repository.Root(), ".restore-rollback-partial", "config")
	if err := os.MkdirAll(rollback, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "site.yaml"), []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	if data, err := repository.ReadFile("content/posts/sentinel"); err != nil || string(data) != "keep" {
		t.Fatalf("untouched root was removed: %q, %v", data, err)
	}
}

func assertNoRestoreArtifacts(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".restore-") {
			t.Fatalf("restore artifact remains: %s", entry.Name())
		}
	}
}

func replacePublicReleaseForTest(t *testing.T, root, target string) {
	t.Helper()
	generated := filepath.Join(root, "generated")
	if err := os.MkdirAll(filepath.Join(generated, filepath.FromSlash(target)), 0o750); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(generated, "current")
	if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.FromSlash(target), current); err != nil {
		t.Fatal(err)
	}
}
