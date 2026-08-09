package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
)

func TestCompareTrees(t *testing.T) {
	current, candidate := t.TempDir(), t.TempDir()
	if err := fsutil.AtomicWrite(filepath.Join(current, "changed.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.AtomicWrite(filepath.Join(current, "extra.txt"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.AtomicWrite(filepath.Join(candidate, "changed.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.AtomicWrite(filepath.Join(candidate, "missing.txt"), []byte("missing"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffs, err := compareTrees(current, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 3 || diffs[0].Kind != "changed" || diffs[1].Kind != "extra" || diffs[2].Kind != "missing" {
		t.Fatalf("diffs=%#v", diffs)
	}
	if _, err := os.Stat(filepath.Join(current, "missing.txt")); !os.IsNotExist(err) {
		t.Fatal("comparison must not modify current output")
	}
}

func TestImportMarkdownToleratesMissingFrontMatterAndDuplicateSlugs(t *testing.T) {
	front, body, err := importMarkdown(importFile{Name: "Hello World.md", Data: []byte("external body"), ModTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, "en", "")
	if err != nil || front.Title != "Hello World" || front.Slug != "hello-world" || front.Status != model.StatusDraft || body != "external body" {
		t.Fatalf("front=%#v body=%q err=%v", front, body, err)
	}
	front, body, err = importMarkdown(importFile{Name: "ignored.md", Data: []byte("---\ntitle: Imported\nslug: same\nstatus: published\ncategories: [News]\n---\ncontent\n")}, "en", model.StatusDraft)
	if err != nil || front.Slug != "same" || front.Status != model.StatusDraft || len(front.Categories) != 1 || body != "content\n" {
		t.Fatalf("front=%#v body=%q err=%v", front, body, err)
	}
	used := map[string]bool{"same": true, "same-2": true}
	if got := uniqueImportSlug("same", used); got != "same-3" {
		t.Fatalf("unique slug=%q", got)
	}
	front, body, err = importMarkdown(importFile{Name: "hugo.md", Data: []byte("+++\ntitle = \"Hugo Post\"\ndate = \"2024-02-03\"\ndraft = false\ncategories = [\"News\", \"Tech\"]\ntags = [\"go\"]\n+++\nHugo body\n")}, "en", "")
	if err != nil || front.Title != "Hugo Post" || front.Slug != "hugo-post" || front.Status != model.StatusPublished || len(front.Categories) != 2 || len(front.Tags) != 1 || body != "Hugo body\n" {
		t.Fatalf("hugo front=%#v body=%q err=%v", front, body, err)
	}
}

func TestReadImportFilesAcceptsDirectoryAndZip(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.md"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := readImportFiles(root)
	if err != nil || len(files) != 1 || files[0].Name != "one.md" {
		t.Fatalf("directory files=%#v err=%v", files, err)
	}
	archivePath := filepath.Join(root, "posts.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, err := writer.Create("nested/two.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	files, err = readImportFiles(archivePath)
	if err != nil || len(files) != 1 || files[0].Name != "nested/two.md" {
		t.Fatalf("zip files=%#v err=%v", files, err)
	}
}

func TestImportCreatesAndReusesTaxonomyNames(t *testing.T) {
	root := t.TempDir()
	store := taxonomy.NewStore(root)
	categories := map[string]*model.Category{}
	resolved, created, err := resolveImportCategories([]string{"News", "News"}, "en", categories, store, false)
	if err != nil || len(created) != 1 || len(resolved) != 1 || resolved[0] != "news" {
		t.Fatalf("categories=%v created=%v err=%v", resolved, created, err)
	}
	if _, err := os.Stat(filepath.Join(root, "categories", "news.yaml")); err != nil {
		t.Fatal(err)
	}
	resolved, created, err = resolveImportCategories([]string{"News"}, "en", categories, store, false)
	if err != nil || len(created) != 0 || resolved[0] != "news" {
		t.Fatalf("reuse=%v created=%v err=%v", resolved, created, err)
	}
	tags := map[string]*model.Tag{}
	resolved, created, err = resolveImportTags([]string{"Go"}, "en", tags, store, false)
	if err != nil || len(created) != 1 || resolved[0] != "go" {
		t.Fatalf("tags=%v created=%v err=%v", resolved, created, err)
	}
}

func TestMigrateCommandBacksUpAndAdvancesContentSchema(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	configData := `schemaVersion: 1
server: { baseURL: https://example.test, port: 8080 }
paths: { content: ./content, data: ./data, media: ./media }
i18n: { defaultLocale: en, sourceLocale: en, locales: [{code: en, name: English, urlPrefix: en, enabled: true}] }
security: { sessionSecret: "01234567890123456789012345678901" }
backup: { dir: ./backups, keep: 2, includeMedia: false }
`
	if err := os.WriteFile(filepath.Join(root, "config", "config.yaml"), []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	store := content.NewStore(filepath.Join(root, "content"))
	article, err := store.CreateBundle(model.ContentPost, "en", model.FrontMatter{Title: "Old", Slug: "old", Author: "admin", SourceLocale: "en", Status: model.StatusDraft, Date: time.Now()}, "body")
	if err != nil {
		t.Fatal(err)
	}
	article.CreatedAt = time.Time{}
	if err := store.SaveMetadata(article); err != nil {
		t.Fatal(err)
	}
	if err := migrateCommand(options{root: root}, []string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if err := migrateCommand(options{root: root}, nil); err != nil {
		t.Fatal(err)
	}
	updated, _, err := config.Load(root, "")
	if err != nil || updated.SchemaVersion != 2 {
		t.Fatalf("schema=%d err=%v", updated.SchemaVersion, err)
	}
	backups, err := filepath.Glob(filepath.Join(root, "backups", "*pre-migration-v2.zip"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	loaded, err := store.LoadBundle(article.BundleDir)
	if err != nil || loaded.CreatedAt.IsZero() {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestConfigValueAndSetPreserveValidConfig(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	data := `# retained comment
server:
  host: 127.0.0.1
  port: 18080
  baseURL: http://localhost:18080
paths:
  content: ./content
  data: ./data
  media: ./media
  themes: ./themes
  generated: ./generated
  cache: ./cache
i18n:
  defaultLocale: en
  sourceLocale: en
  locales:
    - code: en
      name: English
      urlPrefix: en
      enabled: true
security:
  sessionSecret: 0123456789abcdef0123456789abcdef
render:
  workerEnabled: false
  workerCommand: [node, ./renderer/server.js]
  workerSocket: /private/tmp/mutiblog-e2e-render.sock
  output: ./generated/public
  timeout: 30s
`
	if err := fsutil.AtomicWrite(configFile, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := options{root: root}
	value, err := configValue(opts, "server.port")
	if err != nil || value.Value != "18080" {
		t.Fatalf("value=%#v err=%v", value, err)
	}
	if err := setConfigValue(opts, "server.port", "19090"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "# retained comment") {
		t.Fatalf("comment was not preserved:\n%s", updated)
	}
	value, err = configValue(opts, "server.port")
	if err != nil || value.Value != "19090" {
		t.Fatalf("updated value=%#v err=%v", value, err)
	}
	before := string(updated)
	if err := setConfigValue(opts, "server.port", "70000"); err == nil {
		t.Fatal("invalid port unexpectedly accepted")
	}
	after, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatal("invalid set modified the live config")
	}
	if !secretConfigPath("ai.apiKey") || !secretConfigPath("security.sessionSecret") || secretConfigPath("site.title") {
		t.Fatal("secret path classification is incorrect")
	}
}
