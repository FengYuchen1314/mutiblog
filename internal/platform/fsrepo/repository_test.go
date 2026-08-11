package fsrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSecuresExistingConfigDirectory(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	if err := os.Mkdir(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("config mode = %o, want 700", got)
	}
}

func TestOpenRejectsSymlinkInRepositoryLayout(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "content")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("expected repository layout symlink to be rejected")
	}
}

func TestRepositoryRejectsSymlinkIntroducedAfterOpen(t *testing.T) {
	root := t.TempDir()
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	posts := filepath.Join(root, "content", "posts")
	if err := os.Remove(posts); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, posts); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("content/posts/escaped.md", []byte("outside"), 0o640); err == nil {
		t.Fatal("expected runtime repository symlink to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.md")); !os.IsNotExist(err) {
		t.Fatalf("outside path was written through symlink: %v", err)
	}
}

func TestRemoveTreeRejectsSymlinkIntroducedAfterOpen(t *testing.T) {
	root := t.TempDir()
	repository, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	posts := filepath.Join(root, "content", "posts")
	if err := os.Remove(posts); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideContent := filepath.Join(outside, "keep")
	if err := os.WriteFile(outsideContent, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, posts); err != nil {
		t.Fatal(err)
	}
	if err := repository.RemoveTree("content/posts/keep"); err == nil {
		t.Fatal("expected tree removal through a runtime symlink to be rejected")
	}
	if _, err := os.Stat(outsideContent); err != nil {
		t.Fatalf("outside content changed through symlink: %v", err)
	}
}
