package content

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/model"
)

func TestSerializeRoundTripPreservesKnownAndExtraFields(t *testing.T) {
	date := time.Date(2026, 8, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	f := model.FrontMatter{ID: "019fd210-e463-7709-9a23-9252a081279d", Title: "Title", Slug: "title", Date: date, Status: model.StatusPublished, Author: "admin", SourceLocale: "zh-CN", Locale: "zh-CN", Categories: []string{"linux"}, Extra: map[string]any{"custom": "value", "weight": 2}}
	first, err := Serialize(f, "body\n\n")
	if err != nil {
		t.Fatal(err)
	}
	parsed, body, err := Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Serialize(parsed, body)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("round trip differs:\n%s\n---\n%s", first, second)
	}
	if parsed.Extra["custom"] != "value" {
		t.Fatalf("extra=%#v", parsed.Extra)
	}
}

func TestCreateLoadAndScanBundle(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	f := model.FrontMatter{Title: "中文标题", Slug: "中文标题", Status: model.StatusDraft, Author: "admin", SourceLocale: "zh-CN"}
	a, err := s.CreateBundle(model.ContentPost, "zh-CN", f, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.BundleDir == "" {
		t.Fatalf("article=%#v", a)
	}
	loaded, err := s.LoadBundle(a.BundleDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != a.ID || loaded.Versions["zh-CN"].Body != "hello\n" {
		t.Fatalf("loaded=%#v", loaded)
	}
	items, problems, err := s.ScanAll(context.Background())
	if err != nil || len(problems) != 0 || len(items) != 1 {
		t.Fatalf("items=%d errors=%v err=%v", len(items), problems, err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(a.BundleDir), "metadata.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionCanBeReadAndRestoredAsANewRevision(t *testing.T) {
	store := NewStore(t.TempDir())
	article, err := store.CreateBundle(model.ContentPost, "en", model.FrontMatter{Title: "Post", Slug: "post", Status: model.StatusDraft, Author: "admin", SourceLocale: "en"}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotRevision(article, "en"); err != nil {
		t.Fatal(err)
	}
	current := article.Versions["en"]
	if err := store.SaveVersion(article, "en", current.Front, "second", SaveOpts{BumpSourceRevision: true, Snapshot: true}); err != nil {
		t.Fatal(err)
	}
	front, body, err := store.ReadRevision(article.ID, "en", 1)
	if err != nil || body != "first\n" {
		t.Fatalf("revision body=%q err=%v", body, err)
	}
	if err := store.SaveVersion(article, "en", front, body, SaveOpts{BumpSourceRevision: true, Snapshot: true}); err != nil {
		t.Fatal(err)
	}
	if got := article.Versions["en"].Body; got != "first\n" {
		t.Fatalf("restored body=%q", got)
	}
	revisions, err := store.ListRevisions(article.ID, "en")
	if err != nil || len(revisions) != 3 {
		t.Fatalf("revisions=%v err=%v", revisions, err)
	}
}

func TestDeleteBundleRemovesRevisionsAndDrafts(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	article, err := store.CreateBundle(model.ContentPost, "en", model.FrontMatter{Title: "Post", Slug: "post", Status: model.StatusDraft, Author: "admin", SourceLocale: "en"}, "body")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotRevision(article, "en"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDraft(article.ID, "en", article.Versions["en"].Front, "draft body"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteBundle(article); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, filepath.FromSlash(article.BundleDir)),
		filepath.Join(root, ".revisions", string(article.ID)),
		filepath.Join(root, ".drafts", string(article.ID)+".en.md"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted artifact %s still exists: %v", path, err)
		}
	}
}
