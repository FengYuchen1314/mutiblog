package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndRecreatesCorruptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if !errors.Is(err, ErrRecreated) {
		t.Fatalf("err=%v", err)
	}
	if db == nil {
		t.Fatal("nil database")
	}
	defer db.Close()
	var n int
	if err = db.Read().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil || n != 1 {
		t.Fatalf("migrations=%d err=%v", n, err)
	}
}
