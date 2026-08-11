package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

type fakeRenderer struct {
	fail  bool
	input BuildInput
}

func (renderer *fakeRenderer) Render(_ context.Context, inputPath, outputPath string) (BuildReport, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return BuildReport{}, err
	}
	if err := json.Unmarshal(data, &renderer.input); err != nil {
		return BuildReport{}, err
	}
	if renderer.fail {
		return BuildReport{}, errors.New("synthetic renderer failure")
	}
	if err := os.MkdirAll(filepath.Join(outputPath, "zh-CN"), 0o750); err != nil {
		return BuildReport{}, err
	}
	for name, body := range map[string]string{
		"index.html":                         "root redirect",
		"redirects.json":                     "[]",
		"build-report.json":                  "{}",
		filepath.Join("zh-CN", "index.html"): "rendered site",
	} {
		if err := os.WriteFile(filepath.Join(outputPath, name), []byte(body), 0o640); err != nil {
			return BuildReport{}, err
		}
	}
	return BuildReport{SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Files: 4, Locales: []string{"zh-CN"}}, nil
}

func TestBuildActivatesCompleteReleaseAndRecordsTask(t *testing.T) {
	repository, contentService := publisherFixture(t)
	renderer := &fakeRenderer{}
	service := NewService(repository, contentService, renderer)
	report, err := service.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 4 || len(renderer.input.Posts) != 1 || renderer.input.Posts[0].SourceLocale != "zh-CN" || renderer.input.Posts[0].Status != domain.ContentStatusPublished || renderer.input.Posts[0].Template != "post" {
		t.Fatalf("report/input = %#v / %#v", report, renderer.input)
	}
	localized := renderer.input.Posts[0].Locales["zh-CN"]
	if localized.SEOTitle != "发布快照 SEO" || localized.SEODescription != "发布快照描述" {
		t.Fatalf("SEO fields were not preserved in renderer input: %#v", localized)
	}
	current := filepath.Join(repository.Root(), "generated", "current")
	info, err := os.Lstat(current)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("current mode = %v, want symlink", info.Mode())
	}
	data, err := os.ReadFile(filepath.Join(current, "zh-CN", "index.html"))
	if err != nil || string(data) != "rendered site" {
		t.Fatalf("current release = %q, err = %v", data, err)
	}
	tasks, err := repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %v, err = %v", tasks, err)
	}
	var task Task
	if err := repository.ReadYAML(filepath.Join("state", "tasks", tasks[0].Name()), &task); err != nil {
		t.Fatal(err)
	}
	if task.Status != "succeeded" || task.Report == nil || task.CompletedAt == nil {
		t.Fatalf("task = %#v", task)
	}
}

func TestFailedBuildPreservesCurrentRelease(t *testing.T) {
	repository, contentService := publisherFixture(t)
	renderer := &fakeRenderer{}
	service := NewService(repository, contentService, renderer)
	if _, err := service.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(repository.Root(), "generated", "current")
	before, err := os.Readlink(current)
	if err != nil {
		t.Fatal(err)
	}
	renderer.fail = true
	if _, err := service.Build(context.Background()); err == nil {
		t.Fatal("failed renderer unexpectedly succeeded")
	}
	after, err := os.Readlink(current)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("current changed after failure: %q -> %q", before, after)
	}
}

func TestBuildOutputRejectsSymbolicLinks(t *testing.T) {
	stage := t.TempDir()
	for _, name := range []string{"index.html", "redirects.json", "build-report.json"} {
		if err := os.WriteFile(filepath.Join(stage, name), []byte("{}"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stage, "asset.txt")); err != nil {
		t.Fatal(err)
	}
	if err := requireBuildOutputs(stage); err == nil {
		t.Fatal("symbolic link in build output was accepted")
	}
}

func TestThemePreviewCreatesIsolatedExpiringRelease(t *testing.T) {
	repository, contentService := publisherFixture(t)
	renderer := &fakeRenderer{}
	service := NewService(repository, contentService, renderer)
	record, err := service.Preview(context.Background(), "earth", "https://preview.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !validPreviewID(record.ID) || record.ThemeID != "earth" || !record.ExpiresAt.After(record.CreatedAt) {
		t.Fatalf("preview record = %#v", record)
	}
	if renderer.input.BaseURL != "https://preview.example.com" || renderer.input.Theme.ID != "earth" {
		t.Fatalf("preview input = %#v", renderer.input)
	}
	loaded, root, err := service.PreviewRoot(record.ID)
	if err != nil || loaded.ID != record.ID {
		t.Fatalf("preview root = %#v, %q, %v", loaded, root, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "zh-CN", "index.html")); err != nil || string(data) != "rendered site" {
		t.Fatalf("preview output = %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(repository.Root(), "generated", "current")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview unexpectedly changed current release: %v", err)
	}
	if _, _, err := service.PreviewRoot("not-a-preview"); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("invalid preview error = %v", err)
	}
}

func TestPruneStaticReleasesKeepsCurrentAndNewest(t *testing.T) {
	generatedRoot := t.TempDir()
	releasesRoot := filepath.Join(generatedRoot, "releases")
	for _, id := range []string{"001", "002", "003", "004", "005"} {
		if err := os.MkdirAll(filepath.Join(releasesRoot, id), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneStaticReleases(generatedRoot, "002", 3); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(releasesRoot)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := []string{"002", "004", "005"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retained releases = %v, want %v", got, want)
	}
}

func TestPruneStaticReleasesKeepsProtectedRollbackRelease(t *testing.T) {
	generatedRoot := t.TempDir()
	releasesRoot := filepath.Join(generatedRoot, "releases")
	for _, id := range []string{"001", "002", "003", "004", "005"} {
		if err := os.MkdirAll(filepath.Join(releasesRoot, id), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneStaticReleases(generatedRoot, "005", 3, "001"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(releasesRoot)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := []string{"001", "004", "005"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retained releases = %v, want %v", got, want)
	}
}

func TestPruneBuildTasksPreservesRunningAndTranslationTasks(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []Task{
		{SchemaVersion: 1, ID: "20260811T010201.000000000Z-aabbcc01", Kind: "StaticBuild", Status: "succeeded"},
		{SchemaVersion: 1, ID: "20260811T010202.000000000Z-aabbcc02", Kind: "StaticBuild", Status: "failed"},
		{SchemaVersion: 1, ID: "20260811T010203.000000000Z-aabbcc03", Kind: "StaticBuild", Status: "running"},
		{SchemaVersion: 1, ID: "translation-example", Kind: "Translation", Status: "succeeded"},
		{SchemaVersion: 1, ID: "20260811T010205.000000000Z-aabbcc05", Kind: "StaticBuild", Status: "succeeded"},
	} {
		if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneBuildTasks(repository, 2); err != nil {
		t.Fatal(err)
	}
	entries, err := repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := []string{"20260811T010202.000000000Z-aabbcc02.yaml", "20260811T010203.000000000Z-aabbcc03.yaml", "20260811T010205.000000000Z-aabbcc05.yaml", "translation-example.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retained tasks = %v, want %v", got, want)
	}
}

func TestRecoverMarksBuildInterruptedAndCleansDerivedOrphans(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	task := Task{SchemaVersion: 1, ID: "20260811T010203.000000000Z-aabbccdd", Kind: "StaticBuild", Status: "running", StartedAt: time.Now().UTC()}
	if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.Root(), "generated", "staging", "orphan.input.json"), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository.Root(), "generated", "previews", "orphan"), 0o750); err != nil {
		t.Fatal(err)
	}
	count, err := service.Recover()
	if err != nil || count != 3 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	if err := repository.ReadYAML(filepath.Join("state", "tasks", task.ID+".yaml"), &task); err != nil || task.Status != "failed" || task.Error != "interrupted" || task.CompletedAt == nil {
		t.Fatalf("recovered task = %#v, %v", task, err)
	}
	if entries, err := repository.ReadDir(filepath.Join("generated", "staging")); err != nil || len(entries) != 0 {
		t.Fatalf("staging after recovery = %v, %v", entries, err)
	}
}

func TestRecoverIgnoresValidForeignTasksInSharedDirectory(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	for _, task := range []Task{
		{SchemaVersion: 1, ID: "translation-example", Kind: "Translation", Status: "queued"},
		{SchemaVersion: 1, ID: "backup-task-example", Kind: "Backup", Status: "running"},
	} {
		if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
			t.Fatal(err)
		}
	}
	count, err := service.Recover()
	if err != nil || count != 0 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
}

func TestRecoverRejectsUnknownSharedTaskKind(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	task := Task{SchemaVersion: 1, ID: "unknown", Kind: "Unknown", Status: "running"}
	if err := repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Recover(); err == nil {
		t.Fatal("expected an unknown shared task kind to be reported")
	}
}

func TestTaxonomySnapshotPreservesLocalizedSEO(t *testing.T) {
	items := []domain.Taxonomy{{
		ID:           "engineering",
		SourceLocale: "en",
		Locales: map[string]domain.LocalizedTaxonomy{
			"en": {
				Name:           "Engineering",
				Description:    "Technical writing",
				SEOTitle:       "Engineering articles",
				SEODescription: "Technical articles from the engineering team",
			},
		},
	}}

	result := toTaxonomyInputs(items)
	if len(result) != 1 {
		t.Fatalf("taxonomy inputs = %d, want 1", len(result))
	}
	localized := result[0].Locales["en"]
	if result[0].SourceLocale != "en" || localized.SEOTitle != "Engineering articles" || localized.SEODescription != "Technical articles from the engineering team" {
		t.Fatalf("localized taxonomy SEO was lost: %#v", localized)
	}
}

func publisherFixture(t *testing.T) (*fsrepo.Repository, *content.Service) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	site := domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", AdminLocale: "zh-CN", Timezone: "Asia/Shanghai", ActiveTheme: "earth", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "MutiBlog Test"}}, CreatedAt: now, UpdatedAt: now}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}}, Fallback: []string{"en", "zh-CN"}}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "hello-world", Title: "你好", Markdown: "# 正文"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.UpdateLocale(post.Meta.ID, "zh-CN", content.UpdateLocaleInput{
		ExpectedRevision: post.Meta.Revision,
		Title:            "你好",
		SEOTitle:         "发布快照 SEO",
		SEODescription:   "发布快照描述",
		Markdown:         "# 正文",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentService.PublishPost(post.Meta.ID, post.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	return repository, contentService
}
