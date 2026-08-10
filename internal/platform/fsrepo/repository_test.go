package fsrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryWritesAndReadsYAML(t *testing.T) {
	repository, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": "MutiBlog", "schemaVersion": 1}
	if err := repository.WriteYAML("config/site.yaml", want, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := repository.ReadYAML("config/site.yaml", &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != want["name"] {
		t.Fatalf("name = %v, want %v", got["name"], want["name"])
	}
}

func TestRepositoryRejectsEscapes(t *testing.T) {
	repository, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("../escape", []byte("no"), 0o600); err == nil {
		t.Fatal("expected path escape to be rejected")
	}
}

func TestSecretFileMode(t *testing.T) {
	repository, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", map[string]any{"key": "value"}, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(repository.Root(), "config", "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
