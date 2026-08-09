package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	unsafe := []string{"../x", "a/../../x", "/etc/passwd", "..", "", "a\\..\\x", "./../x", "a/../../../x", "./", "//etc/passwd"}
	for _, path := range unsafe {
		if _, err := SafeJoin(base, path); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%q: %v", path, err)
		}
	}
	got, err := SafeJoin(base, "nested/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(realBase, "nested/file.txt") {
		t.Fatalf("got %q", got)
	}
}
func TestSafeJoinRejectsEscapingSymlink(t *testing.T) {
	base := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(base, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := SafeJoin(base, "escape/secret"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err=%v", err)
	}
}
func TestAtomicWriteDoesNotLeaveTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "value.txt")
	if err := AtomicWrite(path, []byte("complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "complete" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	items, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if filepath.Ext(item.Name()) == ".tmp" {
			t.Fatalf("temporary file remained: %s", item.Name())
		}
	}
}
