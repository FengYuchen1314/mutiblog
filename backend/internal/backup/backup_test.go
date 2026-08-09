package backup

import (
	"archive/zip"
	"github.com/fengyuchen/mutiblog/internal/state"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateValidateRedactAndPrune(t *testing.T) {
	root := t.TempDir()
	configContent := "ai:\n  apiKey: secret\nsecurity:\n  sessionSecret: secret-session\n" +
		"cache:\n  apiToken: cache-token\n"
	files := []struct{ path, content string }{
		{"content/posts/a.md", "body"},
		{"data/categories.yaml", "items: []"},
		{"config/config.yaml", configContent},
		{"config/.secrets.yaml", "ai: secret-file\n"},
		{"media/a.txt", "media"},
	}
	for _, item := range files {
		path := filepath.Join(root, item.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(item.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := state.Open(filepath.Join(root, "data", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	logInsert := "INSERT INTO system_log(level,component,message,created_at) " +
		"VALUES('info','test','snapshot',datetime('now'))"
	if _, err = db.Write().Exec(logInsert); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Content:        filepath.Join(root, "content"),
		Data:           filepath.Join(root, "data"),
		Config:         filepath.Join(root, "config"),
		Media:          filepath.Join(root, "media"),
		OutputDir:      filepath.Join(root, "backups"),
		IncludeMedia:   true,
		ExcludeSecrets: true,
		Keep:           1,
	}
	archive, err := Create(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(archive.Path); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	foundSnapshot := false
	for _, file := range reader.File {
		if file.Name == "data/state.db" {
			foundSnapshot = true
		}
		if file.Name == "config/.secrets.yaml" {
			t.Fatal("secret file was included")
		}
		if file.Name == "config/config.yaml" {
			r, _ := file.Open()
			data := make([]byte, file.UncompressedSize64)
			_, _ = r.Read(data)
			r.Close()
			if strings.Contains(string(data), "secret") || strings.Contains(string(data), "cache-token") ||
				!strings.Contains(string(data), "apiKey: \"\"") {
				t.Fatalf("secret leaked: %s", data)
			}
		}
	}
	if !foundSnapshot {
		t.Fatal("state database snapshot is missing")
	}
	if _, err := Create(opts); err != nil {
		t.Fatal(err)
	}
	items, err := List(opts.OutputDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestRestoreRequiresConfirmationAndReplacesCanonicalData(t *testing.T) {
	source := t.TempDir()
	content := filepath.Join(source, "content", "posts", "post.md")
	if err := os.MkdirAll(filepath.Dir(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, err := Create(
		Options{Content: filepath.Join(source, "content"), OutputDir: filepath.Join(source, "backups")},
	)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	old := filepath.Join(target, "content", "posts", "post.md")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restore(RestoreOptions{Root: target, Archive: archive.Path}); err == nil {
		t.Fatal("expected confirmation error")
	}
	if err := Restore(RestoreOptions{Root: target, Archive: archive.Path, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "content", "posts", "post.md"))
	if err != nil || string(got) != "new" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(target, "content.bak.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
}

func TestRestoreInvalidArchiveLeavesLiveDataUntouched(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "content", "posts", "live.md")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(root, "broken.zip")
	if err := os.WriteFile(broken, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restore(RestoreOptions{Root: root, Archive: broken, Confirm: true}); err == nil {
		t.Fatal("invalid archive was restored")
	}
	got, err := os.ReadFile(live)
	if err != nil || string(got) != "live" {
		t.Fatalf("live data changed: %q %v", got, err)
	}
}

func TestCreateSelectedExportScopesAndName(t *testing.T) {
	root := t.TempDir()
	for _, item := range []struct{ path, value string }{
		{"content/posts/a.md", "article"},
		{"data/tags/tags.yaml", "tags"},
		{"config/config.yaml", "server: {}\n"},
		{"media/image.png", "image"},
	} {
		path := filepath.Join(root, item.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(item.value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := Create(Options{
		Content: filepath.Join(
			root,
			"content",
		), Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"),
		Media: filepath.Join(root, "media"), OutputDir: filepath.Join(root, "exports"),
		Name: "content-only.zip", Scopes: map[string]bool{"content": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(archive.Path) != "content-only.zip" {
		t.Fatalf("archive path=%q", archive.Path)
	}
	reader, err := zip.OpenReader(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "data/") || strings.HasPrefix(file.Name, "config/") ||
			strings.HasPrefix(file.Name, "media/") {
			t.Fatalf("unexpected scoped file %q", file.Name)
		}
	}
	opts := Options{
		Content:   filepath.Join(root, "content"),
		OutputDir: filepath.Join(root, "exports"),
		Name:      "content-only.zip",
		Scopes:    map[string]bool{"content": true},
	}
	if _, err := Create(opts); err == nil {
		t.Fatal("existing export was overwritten")
	}
}
