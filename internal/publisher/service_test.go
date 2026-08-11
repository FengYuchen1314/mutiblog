package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

type fakeRenderer struct {
	fail  bool
	input BuildInput
}

type blockingRenderer struct {
	started chan struct{}
	exited  chan struct{}
}

func (renderer *blockingRenderer) Render(ctx context.Context, _, _ string) (BuildReport, error) {
	close(renderer.started)
	<-ctx.Done()
	close(renderer.exited)
	return BuildReport{}, ctx.Err()
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
		"build-report.json":                  `{"schemaVersion":1,"generatedAt":"2026-08-11T00:00:00Z","files":4,"locales":["zh-CN"],"redirects":[]}`,
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
	var legacyLocales domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &legacyLocales); err != nil {
		t.Fatal(err)
	}
	legacyLocales.Enabled = []domain.LocaleDefinition{{Code: "fr", Label: "Français", Enabled: true}}
	if err := repository.WriteYAML("config/locales.yaml", legacyLocales, false); err != nil {
		t.Fatal(err)
	}
	renderer := &fakeRenderer{}
	service := NewService(repository, contentService, renderer)
	report, err := service.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 4 || len(renderer.input.Posts) != 1 || renderer.input.Posts[0].SourceLocale != "zh-CN" || renderer.input.Posts[0].Status != domain.ContentStatusPublished || renderer.input.Posts[0].Template != "post" {
		t.Fatalf("report/input = %#v / %#v", report, renderer.input)
	}
	if renderer.input.Timezone != "Asia/Shanghai" || len(renderer.input.Fallback) != 1 || renderer.input.Fallback[0] != "zh-CN" {
		t.Fatalf("normalized site defaults = timezone %q, fallback %#v", renderer.input.Timezone, renderer.input.Fallback)
	}
	if !rendererLocaleEnabled(renderer.input.Locales, "fr") || rendererLocaleEnabled(renderer.input.Locales, "en") || !rendererLocaleEnabled(renderer.input.Locales, "zh-CN") {
		t.Fatalf("normalized renderer locales = %#v", renderer.input.Locales)
	}
	if renderer.input.Comments.MaxLength != 4321 {
		t.Fatalf("comment settings were not preserved in renderer input: %#v", renderer.input.Comments)
	}
	if renderer.input.Site.Logo != "/media/site-logo.webp" {
		t.Fatalf("site logo was not preserved in renderer input: %q", renderer.input.Site.Logo)
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

func TestBuildRetriesCheckpointBeforeFailingWork(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	defer service.Close()
	ctx, err := service.PrepareBuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int64
	service.writeTaskHook = func(task Task) error {
		if task.Status == "running" && task.Progress.Message == "capturing-content-snapshot" {
			attempt := attempts.Add(1)
			if attempt < taskCheckpointAttempts {
				return errors.New("synthetic checkpoint failure")
			}
		}
		return nil
	}
	if _, err := service.Build(ctx); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != taskCheckpointAttempts {
		t.Fatalf("checkpoint attempts = %d, want %d", got, taskCheckpointAttempts)
	}
}

func TestBuildCheckpointExhaustionPersistsFailedTerminalState(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	defer service.Close()
	ctx, err := service.PrepareBuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	task := ctx.Value(preparedBuildTaskContextKey{}).(Task)
	var attempts atomic.Int64
	service.writeTaskHook = func(task Task) error {
		if task.Status == "running" && task.Progress.Message == "capturing-content-snapshot" {
			attempts.Add(1)
			return errors.New("synthetic checkpoint failure")
		}
		return nil
	}
	if _, err := service.Build(ctx); err == nil || !strings.Contains(err.Error(), "persist static build checkpoint") {
		t.Fatalf("Build() error = %v", err)
	}
	stored, err := service.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.CompletedAt == nil || !strings.Contains(stored.Error, "synthetic checkpoint failure") {
		t.Fatalf("failed checkpoint task = %#v", stored)
	}
	if got := attempts.Load(); got != taskCheckpointAttempts {
		t.Fatalf("checkpoint attempts = %d, want %d", got, taskCheckpointAttempts)
	}
}

func TestFailedBuildTerminalPersistenceConvergesWithoutRestart(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{fail: true})
	defer service.Close()
	ctx, err := service.PrepareBuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	task := ctx.Value(preparedBuildTaskContextKey{}).(Task)
	var attempts atomic.Int64
	service.writeTaskHook = func(task Task) error {
		if task.Status == "failed" && attempts.Add(1) <= taskCheckpointAttempts*2 {
			return errors.New("synthetic terminal checkpoint failure")
		}
		return nil
	}
	if _, err := service.Build(ctx); err == nil || !strings.Contains(err.Error(), "synthetic renderer failure") {
		t.Fatalf("Build() error = %v", err)
	}
	stored := waitForBuildTaskStatus(t, service, task.ID, "failed")
	if stored.CompletedAt == nil || !strings.Contains(stored.Error, "synthetic renderer failure") {
		t.Fatalf("persisted failed terminal task = %#v", stored)
	}
	if got := attempts.Load(); got < taskCheckpointAttempts*2+1 {
		t.Fatalf("terminal attempts = %d, want at least %d", got, taskCheckpointAttempts*2+1)
	}
}

func TestActivatedBuildTerminalPersistenceConvergesWithoutChangingBusinessResult(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	defer service.Close()
	ctx, err := service.PrepareBuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	task := ctx.Value(preparedBuildTaskContextKey{}).(Task)
	var attempts atomic.Int64
	service.writeTaskHook = func(task Task) error {
		if task.Status == "succeeded" && attempts.Add(1) <= taskCheckpointAttempts*2 {
			return errors.New("synthetic terminal checkpoint failure")
		}
		return nil
	}
	report, err := service.Build(ctx)
	if err != nil || report.Files != 4 {
		t.Fatalf("Build() = %#v, %v", report, err)
	}
	target, err := os.Readlink(filepath.Join(repository.Root(), "generated", "current"))
	if err != nil || target != filepath.Join("releases", task.ID) {
		t.Fatalf("active release = %q, %v", target, err)
	}
	stored := waitForBuildTaskStatus(t, service, task.ID, "succeeded")
	if stored.CompletedAt == nil || stored.Report == nil || stored.Report.Files != report.Files || stored.Progress.Percent != 100 {
		t.Fatalf("persisted succeeded terminal task = %#v", stored)
	}
	if got := attempts.Load(); got < taskCheckpointAttempts*2+1 {
		t.Fatalf("terminal attempts = %d, want at least %d", got, taskCheckpointAttempts*2+1)
	}
}

func TestTerminalRetryCannotOverwriteNewerTerminalVersion(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	defer service.Close()
	taskID := "20260811T010203.000000000Z-a1b2c3d4"
	failed := Task{SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "StaticBuild", Status: "failed", Error: "old-terminal"}
	succeeded := failed
	succeeded.Status = "succeeded"
	succeeded.Error = ""
	succeeded.Progress = taskstore.Complete(succeeded.Progress, "new-terminal")
	service.writeTaskHook = func(task Task) error {
		if task.Status == "failed" {
			return errors.New("keep old terminal retry pending")
		}
		return nil
	}
	if err := service.persistTerminal(failed); err == nil {
		t.Fatal("old terminal persistence unexpectedly succeeded")
	}
	if err := service.persistTerminal(succeeded); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * terminalRetryInitial)
	var stored Task
	if err := repository.ReadYAML(filepath.Join("state", "tasks", taskID+".yaml"), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status != "succeeded" || stored.Progress.Percent != 100 || stored.Error != "" {
		t.Fatalf("stored terminal task = %#v", stored)
	}
}

func TestCloseCancelsRendererAndWaitsForBuild(t *testing.T) {
	repository, contentService := publisherFixture(t)
	renderer := &blockingRenderer{started: make(chan struct{}), exited: make(chan struct{})}
	service := NewService(repository, contentService, renderer)
	buildDone := make(chan error, 1)
	go func() {
		_, err := service.Build(context.Background())
		buildDone <- err
	}()
	select {
	case <-renderer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("renderer did not start")
	}
	closeDone := make(chan struct{})
	go func() {
		service.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wait for canceled renderer")
	}
	select {
	case <-renderer.exited:
	default:
		t.Fatal("Close returned before renderer exited")
	}
	if err := <-buildDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Build() error = %v, want context cancellation", err)
	}
	if _, err := service.Build(context.Background()); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("Build after Close error = %v", err)
	}
}

func TestCloseStopsTerminalPersistenceWorker(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{fail: true})
	ctx, err := service.PrepareBuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int64
	service.writeTaskHook = func(task Task) error {
		if task.Status == "failed" {
			attempts.Add(1)
			return errors.New("persistent terminal checkpoint failure")
		}
		return nil
	}
	if _, err := service.Build(ctx); err == nil {
		t.Fatal("failed renderer unexpectedly succeeded")
	}
	deadline := time.Now().Add(2 * time.Second)
	for attempts.Load() <= taskCheckpointAttempts && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if attempts.Load() <= taskCheckpointAttempts {
		t.Fatal("background terminal persistence did not start")
	}
	service.Close()
	afterClose := attempts.Load()
	time.Sleep(2 * terminalRetryInitial)
	if got := attempts.Load(); got != afterClose {
		t.Fatalf("terminal persistence continued after Close: %d -> %d", afterClose, got)
	}
}

func waitForBuildTaskStatus(t *testing.T, service *Service, taskID, status string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		task, err := service.GetTask(taskID)
		if err == nil && task.Status == status {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %q did not reach %q: %#v, %v", taskID, status, task, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func rendererLocaleEnabled(definitions []domain.LocaleDefinition, code string) bool {
	for _, definition := range definitions {
		if definition.Code == code {
			return definition.Enabled
		}
	}
	return false
}

func TestBuildUsesPreparedTaskIDThroughStagingAndActivation(t *testing.T) {
	repository, contentService := publisherFixture(t)
	renderer := &fakeRenderer{}
	service := NewService(repository, contentService, renderer)
	taskID := "20260811T010203.123000000Z-a1b2c3d4"
	ctx := WithBuildRequest(context.Background(), BuildRequest{TaskID: taskID, Operation: "publish", SubjectKind: "Post", SubjectID: "hello-world"})
	if _, err := service.Build(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := service.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != taskID || task.Operation != "publish" || task.SubjectKind != "Post" || task.SubjectID != "hello-world" || task.Progress.Percent != 100 {
		t.Fatalf("prepared task = %#v", task)
	}
	target, err := os.Readlink(filepath.Join(repository.Root(), "generated", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join("releases", taskID) {
		t.Fatalf("active release = %q, want prepared task ID %q", target, taskID)
	}
	if _, err := os.Stat(filepath.Join(repository.Root(), "generated", "releases", taskID, "index.html")); err != nil {
		t.Fatalf("prepared release output is missing: %v", err)
	}
}

func TestBuildOrdersPinnedPostsBeforeRegularPosts(t *testing.T) {
	repository, contentService := publisherFixture(t)
	pinnedPost, err := contentService.CreatePost(content.CreatePostInput{ID: "pinned-post", Title: "置顶"})
	if err != nil {
		t.Fatal(err)
	}
	pinned := true
	pinnedPost, err = contentService.UpdatePostSettings(pinnedPost.Meta.ID, content.UpdatePostSettingsInput{ExpectedRevision: pinnedPost.Meta.Revision, Pinned: &pinned})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentService.PublishPost(pinnedPost.Meta.ID, pinnedPost.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	renderer := &fakeRenderer{}
	service := NewService(repository, contentService, renderer)
	if _, err := service.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(renderer.input.Posts) != 2 || renderer.input.Posts[0].ID != pinnedPost.Meta.ID || !renderer.input.Posts[0].Pinned {
		t.Fatalf("post order = %#v", renderer.input.Posts)
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

func TestRecoverMarksAlreadyActivatedBuildSucceeded(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	taskID := "20260811T010203.000000000Z-aabbccde"
	ctx := WithBuildRequest(context.Background(), BuildRequest{TaskID: taskID, Operation: "publish"})
	if _, err := service.Build(ctx); err != nil {
		t.Fatal(err)
	}
	var task Task
	path := filepath.Join("state", "tasks", taskID+".yaml")
	if err := repository.ReadYAML(path, &task); err != nil {
		t.Fatal(err)
	}
	task.Status = "running"
	task.CompletedAt = nil
	task.Report = nil
	task.Progress = taskstore.Advance(task.Progress, "activate", 3, 4, 90, "activating-release")
	if err := repository.WriteYAML(path, task, false); err != nil {
		t.Fatal(err)
	}
	count, err := service.Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	if err := repository.ReadYAML(path, &task); err != nil || task.Status != "succeeded" || task.Report == nil || task.CompletedAt == nil || task.Progress.Percent != 100 {
		t.Fatalf("recovered activated task = %#v, %v", task, err)
	}
}

func TestRecoverIgnoresValidForeignTasksInSharedDirectory(t *testing.T) {
	repository, contentService := publisherFixture(t)
	service := NewService(repository, contentService, &fakeRenderer{})
	for _, task := range []Task{
		{SchemaVersion: 1, ID: "translation-example", Kind: "Translation", Status: "queued"},
		{SchemaVersion: 1, ID: "backup-task-example", Kind: "Backup", Status: "running"},
		{SchemaVersion: 1, ID: "scheduled-publish-20260811T010203.000000000Z-aabbccdd", Kind: "ScheduledPublish", Status: "queued"},
		{SchemaVersion: 1, ID: "index-rebuild-20260811T010203.000000000Z-aabbccdd", Kind: "IndexRebuild", Status: "queued"},
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
		Cover:        "/media/engineering.webp",
		Template:     "masonry",
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
	if result[0].SourceLocale != "en" || result[0].Cover != "/media/engineering.webp" || result[0].Template != "masonry" || localized.SEOTitle != "Engineering articles" || localized.SEODescription != "Technical articles from the engineering team" {
		t.Fatalf("localized taxonomy SEO was lost: %#v", localized)
	}
}

func TestSnapshotExcludesUntranslatedLocalesAndRendersBuildingLocaleAsReady(t *testing.T) {
	repository, contentService := publisherFixture(t)
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
			{Code: "fr", Label: "Français", Enabled: true, Status: domain.LocaleStatusFailed},
			{Code: "de", Label: "Deutsch", Enabled: true, Status: domain.LocaleStatusBuilding},
		},
		Fallback: []string{"zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, contentService, &fakeRenderer{})
	input, err := service.snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Locales) != 2 || input.Locales[0].Code != "de" || input.Locales[0].Status != domain.LocaleStatusReady || input.Locales[1].Code != "zh-CN" {
		t.Fatalf("public snapshot locales = %#v", input.Locales)
	}
}

func publisherFixture(t *testing.T) (*fsrepo.Repository, *content.Service) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	site := domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", AdminLocale: "zh-CN", Timezone: "Asia/Shanghai", Logo: "/media/site-logo.webp", ActiveTheme: "earth", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "MutiBlog Test"}}, CreatedAt: now, UpdatedAt: now}
	// Keep a Chinese-only fixture here so every publisher build proves the
	// fixed safety fallback remains available outside Server startup.
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}}, Fallback: []string{"zh-CN"}}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/comments.yaml", domain.CommentsConfig{SchemaVersion: 1, Moderation: "pending", PageSize: 20, MaxLength: 4321}, false); err != nil {
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
