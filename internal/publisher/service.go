package publisher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	linkservice "github.com/FengYuchen1314/mutiblog/internal/links"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	menuservice "github.com/FengYuchen1314/mutiblog/internal/menus"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
	themeservice "github.com/FengYuchen1314/mutiblog/internal/themes"
)

type LocalizedPost struct {
	Title          string `json:"title"`
	Summary        string `json:"summary,omitempty"`
	SEOTitle       string `json:"seoTitle,omitempty"`
	SEODescription string `json:"seoDescription,omitempty"`
	Markdown       string `json:"markdown"`
}

type PostInput struct {
	ID            string                   `json:"id"`
	SourceLocale  string                   `json:"sourceLocale"`
	Status        domain.ContentStatus     `json:"status"`
	Template      string                   `json:"template"`
	Cover         string                   `json:"cover,omitempty"`
	Pinned        bool                     `json:"pinned,omitempty"`
	PublishedAt   *time.Time               `json:"publishedAt,omitempty"`
	Categories    []string                 `json:"categories"`
	Tags          []string                 `json:"tags"`
	CommentPolicy string                   `json:"commentPolicy"`
	Locales       map[string]LocalizedPost `json:"locales"`
}

type LocalizedTaxonomyInput struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	SEOTitle       string `json:"seoTitle,omitempty"`
	SEODescription string `json:"seoDescription,omitempty"`
}

type TaxonomyInput struct {
	ID           string                            `json:"id"`
	SourceLocale string                            `json:"sourceLocale"`
	ParentID     string                            `json:"parentId,omitempty"`
	Cover        string                            `json:"cover,omitempty"`
	Template     string                            `json:"template,omitempty"`
	Locales      map[string]LocalizedTaxonomyInput `json:"locales"`
}

type ThemeInput struct {
	ID                  string                         `json:"id"`
	ModulePath          string                         `json:"modulePath,omitempty"`
	AssetsPath          string                         `json:"assetsPath,omitempty"`
	Settings            map[string]any                 `json:"settings"`
	LocalizedSettings   map[string]map[string]string   `json:"localizedSettings,omitempty"`
	LocalizableSettings []string                       `json:"localizableSettings,omitempty"`
	PostTemplates       []themeservice.ContentTemplate `json:"postTemplates,omitempty"`
	PageTemplates       []themeservice.ContentTemplate `json:"pageTemplates,omitempty"`
	CategoryTemplates   []themeservice.ContentTemplate `json:"categoryTemplates,omitempty"`
}

type BuildInput struct {
	SchemaVersion int                       `json:"schemaVersion"`
	SourceLocale  string                    `json:"sourceLocale"`
	Timezone      string                    `json:"timezone"`
	BaseURL       string                    `json:"baseUrl,omitempty"`
	PrimaryMenu   string                    `json:"primaryMenu,omitempty"`
	Locales       []domain.LocaleDefinition `json:"locales"`
	Site          struct {
		Logo    string                          `json:"logo,omitempty"`
		Locales map[string]domain.LocalizedSite `json:"locales"`
	} `json:"site"`
	Posts        []PostInput                  `json:"posts"`
	Pages        []PostInput                  `json:"pages"`
	Categories   []TaxonomyInput              `json:"categories"`
	Tags         []TaxonomyInput              `json:"tags"`
	LinkGroups   []domain.LinkGroup           `json:"linkGroups"`
	Links        []domain.Link                `json:"links"`
	Menus        []domain.Menu                `json:"menus"`
	Dictionaries map[string]map[string]string `json:"dictionaries"`
	Comments     domain.CommentsConfig        `json:"comments"`
	Theme        ThemeInput                   `json:"theme"`
	Fallback     []string                     `json:"fallback"`
	GeneratedAt  time.Time                    `json:"generatedAt"`
}

type RedirectRecord struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status int    `json:"status"`
}

type BuildReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	GeneratedAt   string           `json:"generatedAt"`
	Files         int              `json:"files"`
	Locales       []string         `json:"locales"`
	Redirects     []RedirectRecord `json:"redirects"`
}

var ErrPreviewNotFound = errors.New("theme preview not found")

var ErrBuildTaskNotFound = errors.New("static build task not found")

var ErrServiceClosed = errors.New("publisher service is closed")

type PreviewRecord struct {
	SchemaVersion int         `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string      `yaml:"id" json:"id"`
	ThemeID       string      `yaml:"themeId" json:"themeId"`
	CreatedAt     time.Time   `yaml:"createdAt" json:"createdAt"`
	ExpiresAt     time.Time   `yaml:"expiresAt" json:"expiresAt"`
	Report        BuildReport `yaml:"report" json:"report"`
	URL           string      `yaml:"-" json:"url,omitempty"`
}

type Renderer interface {
	Render(context.Context, string, string) (BuildReport, error)
}

type buildRequestContextKey struct{}
type preparedBuildTaskContextKey struct{}

// BuildRequest supplies durable task metadata without changing the small
// SitePublisher interface used by the server and tests. TaskID may be generated
// by the console before the mutating request starts, allowing that same page to
// poll progress while the HTTP request is still running.
type BuildRequest struct {
	TaskID       string
	Operation    string
	SubjectKind  string
	SubjectID    string
	ParentTaskID string
}

func WithBuildRequest(ctx context.Context, request BuildRequest) context.Context {
	return context.WithValue(ctx, buildRequestContextKey{}, request)
}

func BuildRequestFromContext(ctx context.Context) BuildRequest {
	request, _ := ctx.Value(buildRequestContextKey{}).(BuildRequest)
	return request
}

type Service struct {
	repository      *fsrepo.Repository
	content         *content.Service
	renderer        Renderer
	prepareMu       sync.Mutex
	mu              sync.Mutex
	writeMu         sync.Mutex
	writeTaskHook   func(Task) error
	root            context.Context
	cancel          context.CancelFunc
	lifecycleMu     sync.Mutex
	closed          bool
	closeOnce       sync.Once
	workWG          sync.WaitGroup
	terminalMu      sync.Mutex
	terminalClosed  bool
	terminalRetries map[string]terminalRetry
	terminalWG      sync.WaitGroup
}

const (
	retainedStaticReleases = 20
	retainedBuildTasks     = 200
	retainedThemePreviews  = 10
	rendererTimeout        = 2 * time.Minute
	themePreviewTTL        = 30 * time.Minute
	maxBuildFiles          = 100000
	maxBuildBytes          = int64(1 << 30)
	taskCheckpointAttempts = 3
	terminalRetryInitial   = 100 * time.Millisecond
	terminalRetryMaximum   = 5 * time.Second
)

type terminalRetry struct {
	task    Task
	version uint64
	running bool
}

type Task struct {
	SchemaVersion int                `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string             `yaml:"id" json:"id"`
	Kind          string             `yaml:"kind" json:"kind"`
	Operation     string             `yaml:"operation,omitempty" json:"operation,omitempty"`
	SubjectKind   string             `yaml:"subjectKind,omitempty" json:"subjectKind,omitempty"`
	SubjectID     string             `yaml:"subjectId,omitempty" json:"subjectId,omitempty"`
	ParentTaskID  string             `yaml:"parentTaskId,omitempty" json:"parentTaskId,omitempty"`
	Status        string             `yaml:"status" json:"status"`
	Progress      taskstore.Progress `yaml:"progress" json:"progress"`
	CreatedAt     time.Time          `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
	StartedAt     time.Time          `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt   *time.Time         `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Report        *BuildReport       `yaml:"report,omitempty" json:"report,omitempty"`
	Error         string             `yaml:"error,omitempty" json:"error,omitempty"`
}

func NewService(repository *fsrepo.Repository, contentService *content.Service, renderer Renderer) *Service {
	root, cancel := context.WithCancel(context.Background())
	return &Service{
		repository: repository, content: contentService, renderer: renderer,
		root: root, cancel: cancel, terminalRetries: make(map[string]terminalRetry),
	}
}

// Close rejects new render work, cancels in-flight renderer processes, waits
// for Build and Preview to leave their activation/cleanup paths, then stops any
// best-effort terminal checkpoint persistence. A task that cannot be flushed
// before Close remains recoverable from its queued/running record and active
// release on the next process start.
func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.closed = true
		s.cancel()
		s.lifecycleMu.Unlock()
		s.workWG.Wait()
		s.terminalMu.Lock()
		s.terminalClosed = true
		s.terminalMu.Unlock()
		s.terminalWG.Wait()
	})
}

func (s *Service) beginWork(parent context.Context) (context.Context, func(), error) {
	finishLifecycle, err := s.beginLifecycle()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	stopRootCancellation := context.AfterFunc(s.root, cancel)
	done := func() {
		stopRootCancellation()
		cancel()
		finishLifecycle()
	}
	return ctx, done, nil
}

func (s *Service) beginLifecycle() (func(), error) {
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return nil, ErrServiceClosed
	}
	s.workWG.Add(1)
	s.lifecycleMu.Unlock()
	return s.workWG.Done, nil
}

func (s *Service) Recover() (int, error) {
	finishLifecycle, err := s.beginLifecycle()
	if err != nil {
		return 0, err
	}
	defer finishLifecycle()
	recovered := 0
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return recovered, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var task Task
		path := filepath.Join("state", "tasks", entry.Name())
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := s.repository.ReadYAML(path, &task); err != nil {
			return recovered, fmt.Errorf("read static build task directory entry %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return recovered, fmt.Errorf("static build task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "StaticBuild" || (task.Status != "queued" && task.Status != "running") {
			continue
		}
		completed := time.Now().UTC()
		report, active, err := s.activatedTaskReport(task.ID)
		if err != nil {
			return recovered, err
		}
		task.CompletedAt = &completed
		if active {
			task.Status = "succeeded"
			task.Error = ""
			task.Report = &report
			task.Progress = taskstore.Complete(task.Progress, "release-active")
		} else {
			task.Status = "failed"
			task.Error = "interrupted"
			task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, task.Progress.Current, task.Progress.Total, task.Progress.Percent, "interrupted")
		}
		if err := s.persistTerminal(task); err != nil {
			// Recovery has already derived the terminal state from durable public
			// facts. Keep recovering sibling tasks and let the versioned retry
			// worker converge this record without making startup repeat cleanup.
			slog.Error("persist recovered terminal static build task failed; retrying in background", "task", task.ID, "status", task.Status, "error", err)
		} else {
			s.pruneTaskHistory()
		}
		recovered++
	}
	stagingRoot := filepath.Join(s.repository.Root(), "generated", "staging")
	stagingEntries, err := os.ReadDir(stagingRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return recovered, err
	}
	for _, entry := range stagingEntries {
		if err := os.RemoveAll(filepath.Join(stagingRoot, entry.Name())); err != nil {
			return recovered, err
		}
		recovered++
	}
	validPreviews := map[string]bool{}
	previewEntries, err := s.repository.ReadDir(filepath.Join("state", "previews"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return recovered, err
	}
	now := time.Now().UTC()
	for _, entry := range previewEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		var record PreviewRecord
		root := filepath.Join(s.repository.Root(), "generated", "previews", id)
		info, statErr := os.Stat(root)
		if err := s.repository.ReadYAML(filepath.Join("state", "previews", entry.Name()), &record); err != nil || record.ID != id || !validPreviewID(id) || !record.ExpiresAt.After(now) || statErr != nil || !info.IsDir() {
			_ = os.Remove(filepath.Join(s.repository.Root(), "state", "previews", entry.Name()))
			_ = os.RemoveAll(root)
			recovered++
			continue
		}
		validPreviews[id] = true
	}
	previewRoot := filepath.Join(s.repository.Root(), "generated", "previews")
	generatedPreviews, err := os.ReadDir(previewRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return recovered, err
	}
	for _, entry := range generatedPreviews {
		if validPreviews[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(previewRoot, entry.Name())); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

// activatedTaskReport closes the crash window between atomically switching
// generated/current and persisting the task's succeeded terminal state.
func (s *Service) activatedTaskReport(taskID string) (BuildReport, bool, error) {
	generatedRoot := filepath.Join(s.repository.Root(), "generated")
	target, err := os.Readlink(filepath.Join(generatedRoot, "current"))
	if errors.Is(err, os.ErrNotExist) {
		return BuildReport{}, false, nil
	}
	if err != nil {
		return BuildReport{}, false, nil
	}
	activeID, valid := releaseIDFromTarget(target)
	if !valid || activeID != taskID {
		return BuildReport{}, false, nil
	}
	releasePath := filepath.Join(generatedRoot, "releases", taskID)
	if err := requireBuildOutputs(releasePath); err != nil {
		return BuildReport{}, false, fmt.Errorf("validate activated release for task recovery: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(releasePath, "build-report.json"))
	if err != nil {
		return BuildReport{}, false, err
	}
	var report BuildReport
	if err := json.Unmarshal(data, &report); err != nil || report.SchemaVersion != domain.SchemaVersion {
		return BuildReport{}, false, errors.New("activated release has an invalid build report")
	}
	return report, true, nil
}

// ListTasks exposes durable static builds to the server's cross-kind task
// center. Every directory entry is identity-checked before a legal foreign kind
// is skipped, preserving the shared task directory's tamper detection.
func (s *Service) ListTasks() ([]Task, error) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		var task Task
		if err := s.repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return nil, fmt.Errorf("read static build task directory entry %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("static build task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "StaticBuild" {
			continue
		}
		if !validStoredBuildTask(task, expectedID) {
			return nil, fmt.Errorf("static build task %q is invalid", expectedID)
		}
		normalizeLegacyBuildTask(&task)
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return buildTaskCreatedAt(tasks[i]).After(buildTaskCreatedAt(tasks[j])) })
	return tasks, nil
}

func (s *Service) GetTask(id string) (Task, error) {
	if !taskstore.ValidStaticBuildID(id) {
		return Task{}, ErrBuildTaskNotFound
	}
	var task Task
	if err := s.repository.ReadYAML(filepath.Join("state", "tasks", id+".yaml"), &task); err != nil || !validStoredBuildTask(task, id) {
		return Task{}, ErrBuildTaskNotFound
	}
	normalizeLegacyBuildTask(&task)
	return task, nil
}

func validStoredBuildTask(task Task, expectedID string) bool {
	if task.SchemaVersion != domain.SchemaVersion || task.Kind != "StaticBuild" || task.ID != expectedID || !taskstore.ValidStaticBuildID(task.ID) {
		return false
	}
	switch task.Status {
	case "queued", "running", "succeeded", "failed":
		return true
	default:
		return false
	}
}

func normalizeLegacyBuildTask(task *Task) {
	if task.Operation == "" {
		task.Operation = "rebuild"
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = task.StartedAt
	}
	if task.Status == "succeeded" && task.Progress.Percent < 100 {
		task.Progress = taskstore.Complete(task.Progress, "completed")
	}
}

func buildTaskCreatedAt(task Task) time.Time {
	if !task.CreatedAt.IsZero() {
		return task.CreatedAt
	}
	return task.StartedAt
}

// PrepareBuild persists the queued task before any publisher serialization
// gate is acquired. A second build can therefore be observed as queued instead
// of disappearing until the first render finishes.
func (s *Service) PrepareBuild(ctx context.Context) (context.Context, error) {
	if _, ok := ctx.Value(preparedBuildTaskContextKey{}).(Task); ok {
		return ctx, nil
	}
	finishLifecycle, err := s.beginLifecycle()
	if err != nil {
		return ctx, err
	}
	defer finishLifecycle()
	if s.renderer == nil {
		return ctx, errors.New("publisher renderer is not configured")
	}
	s.prepareMu.Lock()
	defer s.prepareMu.Unlock()
	request := BuildRequestFromContext(ctx)
	taskID, err := s.resolveBuildTaskID(request.TaskID)
	if err != nil {
		return ctx, err
	}
	now := time.Now().UTC()
	operation := strings.TrimSpace(request.Operation)
	if operation == "" {
		operation = "rebuild"
	}
	task := Task{
		SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "StaticBuild", Operation: operation,
		SubjectKind: request.SubjectKind, SubjectID: request.SubjectID, ParentTaskID: request.ParentTaskID,
		Status: "queued", CreatedAt: now, Progress: taskstore.Progress{Phase: "queued", Total: 4},
	}
	if err := s.writeTask(task); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, preparedBuildTaskContextKey{}, task), nil
}

func (s *Service) Build(ctx context.Context) (BuildReport, error) {
	workContext, done, err := s.beginWork(ctx)
	if err != nil {
		return BuildReport{}, err
	}
	defer done()
	ctx = workContext
	ctx, err = s.PrepareBuild(ctx)
	if err != nil {
		return BuildReport{}, err
	}
	task, ok := ctx.Value(preparedBuildTaskContextKey{}).(Task)
	if !ok {
		return BuildReport{}, errors.New("static build task was not prepared")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	task.Status = "running"
	task.StartedAt = time.Now().UTC()
	if err := s.advanceTask(&task, "snapshot", 0, 4, 5, "capturing-content-snapshot"); err != nil {
		return BuildReport{}, err
	}
	input, err := s.snapshot("")
	if err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if err := s.advanceTask(&task, "snapshot", 1, 4, 20, "snapshot-ready"); err != nil {
		return BuildReport{}, err
	}
	inputData, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	generatedRoot := filepath.Join(s.repository.Root(), "generated")
	inputPath := filepath.Join(generatedRoot, "staging", task.ID+".input.json")
	stagePath := filepath.Join(generatedRoot, "staging", task.ID)
	if err := os.WriteFile(inputPath, inputData, 0o640); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	defer os.Remove(inputPath)
	defer os.RemoveAll(stagePath)

	if err := s.advanceTask(&task, "render", 1, 4, 25, "rendering-static-pages"); err != nil {
		return BuildReport{}, err
	}
	renderContext, cancelRender := context.WithTimeout(ctx, rendererTimeout)
	defer cancelRender()
	report, err := s.renderer.Render(renderContext, inputPath, stagePath)
	if err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if err := ctx.Err(); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if err := s.advanceTask(&task, "render", 2, 4, 70, "render-complete"); err != nil {
		return BuildReport{}, err
	}
	if report.SchemaVersion != domain.SchemaVersion {
		return BuildReport{}, s.failTask(task, errors.New("renderer returned an unsupported report schema"))
	}
	if err := s.advanceTask(&task, "validate", 2, 4, 75, "validating-static-output"); err != nil {
		return BuildReport{}, err
	}
	if err := requireBuildOutputs(stagePath); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if err := s.advanceTask(&task, "validate", 3, 4, 85, "static-output-valid"); err != nil {
		return BuildReport{}, err
	}
	if err := s.advanceTask(&task, "activate", 3, 4, 90, "activating-release"); err != nil {
		return BuildReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if err := s.activate(task.ID, stagePath); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	completed := time.Now().UTC()
	task.Status = "succeeded"
	task.CompletedAt = &completed
	task.Report = &report
	task.Progress = taskstore.Complete(task.Progress, "release-active")
	if err := s.persistTerminal(task); err != nil {
		// generated/current is already the atomic public commit. Returning an
		// error here would make theme transactions roll configuration back while
		// visitors see the new release. Keep the business result successful while
		// a version-safe background worker converges the durable terminal record;
		// startup recovery remains the final crash fallback.
		slog.Error("persist succeeded static build task after activation failed; retrying in background", "task", task.ID, "error", err)
		return report, nil
	}
	s.pruneTaskHistory()
	return report, nil
}

func (s *Service) Preview(ctx context.Context, themeID, baseURL string) (PreviewRecord, error) {
	workContext, done, err := s.beginWork(ctx)
	if err != nil {
		return PreviewRecord{}, err
	}
	defer done()
	ctx = workContext
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return PreviewRecord{}, err
	}
	if s.renderer == nil {
		return PreviewRecord{}, errors.New("publisher renderer is not configured")
	}
	previewID, err := newPreviewID()
	if err != nil {
		return PreviewRecord{}, err
	}
	input, err := s.snapshot(themeID)
	if err != nil {
		return PreviewRecord{}, err
	}
	input.BaseURL = strings.TrimSuffix(baseURL, "/")
	inputData, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return PreviewRecord{}, err
	}
	generatedRoot := filepath.Join(s.repository.Root(), "generated")
	inputPath := filepath.Join(generatedRoot, "staging", "preview-"+previewID+".input.json")
	stagePath := filepath.Join(generatedRoot, "staging", "preview-"+previewID)
	if err := os.WriteFile(inputPath, inputData, 0o640); err != nil {
		return PreviewRecord{}, err
	}
	defer os.Remove(inputPath)
	defer os.RemoveAll(stagePath)
	renderContext, cancelRender := context.WithTimeout(ctx, rendererTimeout)
	defer cancelRender()
	report, err := s.renderer.Render(renderContext, inputPath, stagePath)
	if err != nil {
		return PreviewRecord{}, err
	}
	if err := ctx.Err(); err != nil {
		return PreviewRecord{}, err
	}
	if report.SchemaVersion != domain.SchemaVersion {
		return PreviewRecord{}, errors.New("renderer returned an unsupported report schema")
	}
	if err := requireBuildOutputs(stagePath); err != nil {
		return PreviewRecord{}, err
	}
	previewPath := filepath.Join(generatedRoot, "previews", previewID)
	if err := os.Rename(stagePath, previewPath); err != nil {
		return PreviewRecord{}, fmt.Errorf("promote theme preview: %w", err)
	}
	now := time.Now().UTC()
	record := PreviewRecord{SchemaVersion: domain.SchemaVersion, ID: previewID, ThemeID: themeID, CreatedAt: now, ExpiresAt: now.Add(themePreviewTTL), Report: report}
	if err := s.repository.WriteYAML(filepath.Join("state", "previews", previewID+".yaml"), record, false); err != nil {
		_ = os.RemoveAll(previewPath)
		return PreviewRecord{}, err
	}
	s.pruneThemePreviews(previewID)
	return record, nil
}

func (s *Service) PreviewRoot(id string) (PreviewRecord, string, error) {
	if !validPreviewID(id) {
		return PreviewRecord{}, "", ErrPreviewNotFound
	}
	var record PreviewRecord
	if err := s.repository.ReadYAML(filepath.Join("state", "previews", id+".yaml"), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PreviewRecord{}, "", ErrPreviewNotFound
		}
		return PreviewRecord{}, "", err
	}
	root := filepath.Join(s.repository.Root(), "generated", "previews", id)
	info, err := os.Stat(root)
	if record.SchemaVersion != domain.SchemaVersion || record.ID != id || !record.ExpiresAt.After(time.Now().UTC()) || err != nil || !info.IsDir() {
		_ = os.RemoveAll(root)
		_ = os.Remove(filepath.Join(s.repository.Root(), "state", "previews", id+".yaml"))
		return PreviewRecord{}, "", ErrPreviewNotFound
	}
	return record, root, nil
}

func (s *Service) pruneThemePreviews(currentID string) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "previews"))
	if err != nil {
		return
	}
	type candidate struct {
		id      string
		expires time.Time
	}
	items := make([]candidate, 0, len(entries))
	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		var record PreviewRecord
		if err := s.repository.ReadYAML(filepath.Join("state", "previews", entry.Name()), &record); err != nil || record.ID != id || !record.ExpiresAt.After(now) {
			_ = os.Remove(filepath.Join(s.repository.Root(), "state", "previews", entry.Name()))
			_ = os.RemoveAll(filepath.Join(s.repository.Root(), "generated", "previews", id))
			continue
		}
		items = append(items, candidate{id: id, expires: record.ExpiresAt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].expires.Before(items[j].expires) })
	for len(items) > retainedThemePreviews {
		item := items[0]
		items = items[1:]
		if item.id == currentID {
			items = append(items, item)
			continue
		}
		_ = os.Remove(filepath.Join(s.repository.Root(), "state", "previews", item.id+".yaml"))
		_ = os.RemoveAll(filepath.Join(s.repository.Root(), "generated", "previews", item.id))
	}
}

func (s *Service) snapshot(themeID string) (BuildInput, error) {
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		return BuildInput{}, err
	}
	var locales domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		return BuildInput{}, err
	}
	localeconfig.Normalize(&locales)
	commentSettings := domain.CommentsConfig{SchemaVersion: domain.SchemaVersion, Moderation: "pending", PageSize: 20, MaxLength: 2000}
	if err := s.repository.ReadYAML("config/comments.yaml", &commentSettings); err != nil && !errors.Is(err, os.ErrNotExist) {
		return BuildInput{}, err
	}
	if commentSettings.MaxLength < 100 || commentSettings.MaxLength > 10000 {
		commentSettings.MaxLength = 2000
	}
	timezone := strings.TrimSpace(site.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	posts, err := s.content.ListPostsForBuild()
	if err != nil {
		return BuildInput{}, err
	}
	input := BuildInput{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  locales.SourceLocale,
		Timezone:      timezone,
		BaseURL:       site.BaseURL,
		PrimaryMenu:   site.PrimaryMenu,
		Fallback:      append([]string(nil), locales.Fallback...),
		GeneratedAt:   time.Now().UTC(),
		Posts:         make([]PostInput, 0, len(posts)),
		Comments:      commentSettings,
	}
	pages, err := s.content.ListPagesForBuild()
	if err != nil {
		return BuildInput{}, err
	}
	input.Pages = make([]PostInput, 0, len(pages))
	taxonomyService := taxonomy.NewService(s.repository)
	categories, err := taxonomyService.List("Category")
	if err != nil {
		return BuildInput{}, err
	}
	tags, err := taxonomyService.List("Tag")
	if err != nil {
		return BuildInput{}, err
	}
	linksService := linkservice.NewService(s.repository)
	linkGroups, err := linksService.ListGroups()
	if err != nil {
		return BuildInput{}, err
	}
	linkItems, err := linksService.ListLinks()
	if err != nil {
		return BuildInput{}, err
	}
	input.LinkGroups = linkGroups
	input.Links = linkItems
	menuItems, err := menuservice.NewService(s.repository).List()
	if err != nil {
		return BuildInput{}, err
	}
	input.Menus = menuItems
	dictionaries, err := dictionary.Read(s.repository)
	if err != nil {
		return BuildInput{}, err
	}
	input.Dictionaries = dictionaries
	themeService := themeservice.NewService(s.repository)
	var themeRuntime themeservice.Runtime
	if themeID == "" {
		themeRuntime, err = themeService.Runtime()
	} else {
		themeRuntime, err = themeService.RuntimeFor(themeID)
	}
	if err != nil {
		return BuildInput{}, err
	}
	input.Theme = ThemeInput{
		ID:                themeRuntime.ID,
		ModulePath:        themeRuntime.ModulePath,
		AssetsPath:        themeRuntime.AssetsPath,
		Settings:          themeRuntime.Settings,
		LocalizedSettings: themeRuntime.LocalizedSettings,
		PostTemplates:     themeRuntime.PostTemplates,
		PageTemplates:     themeRuntime.PageTemplates,
		CategoryTemplates: themeRuntime.CategoryTemplates,
	}
	localizableThemeSettings, err := themeService.LocalizableText(themeRuntime.ID)
	if err != nil {
		return BuildInput{}, err
	}
	for path := range localizableThemeSettings {
		input.Theme.LocalizableSettings = append(input.Theme.LocalizableSettings, path)
	}
	sort.Strings(input.Theme.LocalizableSettings)
	for _, locale := range locales.Enabled {
		// A newly appended locale stays out of every public snapshot until its
		// complete site-wide translation has been committed. Empty status is the
		// backward-compatible representation written by older releases. Building
		// is a private two-phase state: the renderer validates it as ready while
		// public APIs continue to reject it until the atomic build succeeds.
		if locale.Enabled && (locale.Status == "" || locale.Status == domain.LocaleStatusReady || locale.Status == domain.LocaleStatusBuilding) {
			if locale.Status == domain.LocaleStatusBuilding {
				locale.Status = domain.LocaleStatusReady
			}
			input.Locales = append(input.Locales, locale)
		}
	}
	input.Site.Logo = site.Logo
	input.Site.Locales = site.Locales
	for _, post := range posts {
		localized := make(map[string]LocalizedPost, len(post.Content))
		for locale, value := range post.Content {
			localized[locale] = LocalizedPost{Title: value.Title, Summary: value.Summary, SEOTitle: value.SEOTitle, SEODescription: value.SEODescription, Markdown: value.Markdown}
		}
		input.Posts = append(input.Posts, PostInput{ID: post.Meta.ID, SourceLocale: post.Meta.SourceLocale, Status: post.Meta.Status, Template: post.Meta.Template, Cover: post.Meta.Cover, Pinned: post.Meta.Pinned, PublishedAt: post.Meta.PublishedAt, Categories: append([]string(nil), post.Meta.Categories...), Tags: append([]string(nil), post.Meta.Tags...), CommentPolicy: post.Meta.CommentPolicy, Locales: localized})
	}
	for _, page := range pages {
		localized := make(map[string]LocalizedPost, len(page.Content))
		for locale, value := range page.Content {
			localized[locale] = LocalizedPost{Title: value.Title, Summary: value.Summary, SEOTitle: value.SEOTitle, SEODescription: value.SEODescription, Markdown: value.Markdown}
		}
		input.Pages = append(input.Pages, PostInput{ID: page.Meta.ID, SourceLocale: page.Meta.SourceLocale, Status: page.Meta.Status, Template: page.Meta.Template, Cover: page.Meta.Cover, PublishedAt: page.Meta.PublishedAt, Categories: []string{}, Tags: []string{}, CommentPolicy: page.Meta.CommentPolicy, Locales: localized})
	}
	input.Categories = toTaxonomyInputs(categories)
	input.Tags = toTaxonomyInputs(tags)
	sort.Slice(input.Locales, func(i, j int) bool { return input.Locales[i].Code < input.Locales[j].Code })
	sort.SliceStable(input.Posts, func(i, j int) bool {
		if input.Posts[i].Pinned != input.Posts[j].Pinned {
			return input.Posts[i].Pinned
		}
		if input.Posts[i].PublishedAt != nil && input.Posts[j].PublishedAt != nil && !input.Posts[i].PublishedAt.Equal(*input.Posts[j].PublishedAt) {
			return input.Posts[i].PublishedAt.After(*input.Posts[j].PublishedAt)
		}
		if input.Posts[i].PublishedAt != nil || input.Posts[j].PublishedAt != nil {
			return input.Posts[i].PublishedAt != nil
		}
		return input.Posts[i].ID < input.Posts[j].ID
	})
	sort.Slice(input.Pages, func(i, j int) bool { return input.Pages[i].ID < input.Pages[j].ID })
	sort.Slice(input.Categories, func(i, j int) bool { return input.Categories[i].ID < input.Categories[j].ID })
	sort.Slice(input.Tags, func(i, j int) bool { return input.Tags[i].ID < input.Tags[j].ID })
	return input, nil
}

func toTaxonomyInputs(items []domain.Taxonomy) []TaxonomyInput {
	result := make([]TaxonomyInput, 0, len(items))
	for _, item := range items {
		localized := make(map[string]LocalizedTaxonomyInput, len(item.Locales))
		for locale, value := range item.Locales {
			localized[locale] = LocalizedTaxonomyInput{
				Name:           value.Name,
				Description:    value.Description,
				SEOTitle:       value.SEOTitle,
				SEODescription: value.SEODescription,
			}
		}
		result = append(result, TaxonomyInput{ID: item.ID, SourceLocale: item.SourceLocale, ParentID: item.ParentID, Cover: item.Cover, Template: item.Template, Locales: localized})
	}
	return result
}

func (s *Service) activate(buildID, stagePath string) error {
	generatedRoot := filepath.Join(s.repository.Root(), "generated")
	releasePath := filepath.Join(generatedRoot, "releases", buildID)
	if err := os.Rename(stagePath, releasePath); err != nil {
		return fmt.Errorf("promote static release: %w", err)
	}
	currentPath := filepath.Join(generatedRoot, "current")
	previousReleaseID := ""
	if target, err := os.Readlink(currentPath); err == nil {
		previousReleaseID, _ = releaseIDFromTarget(target)
	}
	if info, err := os.Lstat(currentPath); err == nil && info.IsDir() {
		legacyPath := filepath.Join(generatedRoot, "releases", "legacy-"+buildID)
		if err := os.Rename(currentPath, legacyPath); err != nil {
			return fmt.Errorf("migrate legacy static release: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporaryLink := filepath.Join(generatedRoot, ".current-"+buildID)
	_ = os.Remove(temporaryLink)
	if err := os.Symlink(filepath.Join("releases", buildID), temporaryLink); err != nil {
		return fmt.Errorf("create current release link: %w", err)
	}
	if err := os.Rename(temporaryLink, currentPath); err != nil {
		_ = os.Remove(temporaryLink)
		return fmt.Errorf("activate static release: %w", err)
	}
	if err := syncDirectory(generatedRoot); err != nil {
		// The atomic current switch is already the publication commit point. Do
		// not report a failed build after clients can observe the new release;
		// startup recovery validates the active release and reconciles its task.
		slog.Warn("sync activated release directory failed", "error", err)
	}
	if err := pruneStaticReleases(generatedRoot, buildID, retainedStaticReleases, previousReleaseID); err != nil {
		slog.Warn("prune stale static releases failed", "error", err)
	}
	return nil
}

func pruneStaticReleases(generatedRoot, currentID string, retain int, protectedIDs ...string) error {
	if retain < 1 {
		retain = 1
	}
	releasesRoot := filepath.Join(generatedRoot, "releases")
	entries, err := os.ReadDir(releasesRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	kept := map[string]bool{currentID: true}
	for _, id := range protectedIDs {
		if id != "" && filepath.Base(id) == id {
			kept[id] = true
		}
	}
	for index := len(ids) - 1; index >= 0 && len(kept) < retain; index-- {
		kept[ids[index]] = true
	}
	for _, id := range ids {
		if !kept[id] {
			if err := os.RemoveAll(filepath.Join(releasesRoot, id)); err != nil {
				return err
			}
		}
	}
	return syncDirectory(releasesRoot)
}

func releaseIDFromTarget(target string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	if target != clean || !strings.HasPrefix(clean, "releases/") {
		return "", false
	}
	id := strings.TrimPrefix(clean, "releases/")
	return id, id != "" && filepath.Base(id) == id
}

func (s *Service) failTask(task Task, buildErr error) error {
	completed := time.Now().UTC()
	task.Status = "failed"
	task.CompletedAt = &completed
	task.Error = buildErr.Error()
	task.Progress = taskstore.Advance(task.Progress, task.Progress.Phase, task.Progress.Current, task.Progress.Total, task.Progress.Percent, "build-failed")
	if err := s.persistTerminal(task); err != nil {
		// The build's business failure is authoritative. Do not replace it with
		// checkpoint I/O, and do not strand the task as running: persistTerminal
		// has already installed a background retry for the failed state.
		slog.Error("persist failed static build task failed; retrying in background", "task", task.ID, "buildError", buildErr, "error", err)
		return buildErr
	}
	s.pruneTaskHistory()
	return buildErr
}

func (s *Service) advanceTask(task *Task, phase string, current, total, percent int, message string) error {
	task.Progress = taskstore.Advance(task.Progress, phase, current, total, percent, message)
	if err := s.writeTask(*task); err != nil {
		return s.failTask(*task, fmt.Errorf("persist static build checkpoint %q: %w", message, err))
	}
	return nil
}

func (s *Service) resolveBuildTaskID(requested string) (string, error) {
	if requested == "" {
		return newBuildID()
	}
	if !taskstore.ValidStaticBuildID(requested) {
		return "", errors.New("static build task ID is invalid")
	}
	exists, err := s.repository.Exists(filepath.Join("state", "tasks", requested+".yaml"))
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("static build task ID already exists")
	}
	return requested, nil
}

func (s *Service) writeTask(task Task) error {
	if !taskstore.ValidStaticBuildID(task.ID) {
		return ErrBuildTaskNotFound
	}
	var lastErr error
	for attempt := 1; attempt <= taskCheckpointAttempts; attempt++ {
		lastErr = s.writeTaskOnce(task)
		if lastErr == nil {
			return nil
		}
		if attempt < taskCheckpointAttempts {
			slog.Warn("persist static build task failed; retrying", "task", task.ID, "attempt", attempt, "error", lastErr)
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *Service) writeTaskOnce(task Task) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeTaskOnceLocked(task)
}

func (s *Service) writeTaskOnceLocked(task Task) error {
	if s.writeTaskHook != nil {
		if err := s.writeTaskHook(task); err != nil {
			return err
		}
	}
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
}

// persistTerminal registers every terminal state before attempting I/O. The
// monotonically increasing pending version makes an older background retry
// unable to overwrite a newer terminal state, even if the newer state is
// persisted directly while the old worker is between retries.
func (s *Service) persistTerminal(task Task) error {
	if !taskstore.ValidStaticBuildID(task.ID) {
		return ErrBuildTaskNotFound
	}
	if task.Status != "succeeded" && task.Status != "failed" {
		return errors.New("static build terminal status is invalid")
	}
	s.terminalMu.Lock()
	if s.terminalClosed {
		s.terminalMu.Unlock()
		return ErrServiceClosed
	}
	entry := s.terminalRetries[task.ID]
	entry.task = task
	entry.version++
	s.terminalRetries[task.ID] = entry
	version := entry.version
	s.terminalMu.Unlock()

	err := s.writeTerminalVersion(task.ID, version)
	if err == nil || errors.Is(err, errStaleTerminalVersion) {
		return nil
	}
	s.startTerminalRetry(task.ID)
	return err
}

var errStaleTerminalVersion = errors.New("stale terminal task version")

func (s *Service) writeTerminalVersion(taskID string, version uint64) error {
	var lastErr error
	for attempt := 1; attempt <= taskCheckpointAttempts; attempt++ {
		lastErr = s.writeTerminalVersionOnce(taskID, version)
		if lastErr == nil || errors.Is(lastErr, errStaleTerminalVersion) {
			return lastErr
		}
		if attempt < taskCheckpointAttempts {
			slog.Warn("persist terminal static build task failed; retrying", "task", taskID, "attempt", attempt, "error", lastErr)
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
	}
	return lastErr
}

func (s *Service) writeTerminalVersionOnce(taskID string, version uint64) error {
	// Serialize the version check and write with all ordinary checkpoint writes.
	// Holding terminalMu across the atomic repository replacement means a newer
	// version is either registered before this check (so this write is skipped)
	// or after this write (so the newer state necessarily writes last).
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	entry, exists := s.terminalRetries[taskID]
	if !exists || entry.version != version {
		return errStaleTerminalVersion
	}
	if err := s.writeTaskOnceLocked(entry.task); err != nil {
		return err
	}
	delete(s.terminalRetries, taskID)
	return nil
}

func (s *Service) startTerminalRetry(taskID string) {
	s.terminalMu.Lock()
	entry, exists := s.terminalRetries[taskID]
	if !exists || entry.running || s.terminalClosed {
		s.terminalMu.Unlock()
		return
	}
	entry.running = true
	s.terminalRetries[taskID] = entry
	s.terminalWG.Add(1)
	s.terminalMu.Unlock()
	go s.persistTerminalUntilDone(taskID)
}

func (s *Service) persistTerminalUntilDone(taskID string) {
	defer s.terminalWG.Done()
	delay := terminalRetryInitial
	for {
		timer := time.NewTimer(delay)
		select {
		case <-s.root.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.terminalMu.Lock()
		entry, exists := s.terminalRetries[taskID]
		s.terminalMu.Unlock()
		if !exists {
			return
		}
		err := s.writeTerminalVersion(taskID, entry.version)
		if err == nil {
			s.pruneTaskHistory()
			return
		}
		if errors.Is(err, errStaleTerminalVersion) {
			delay = terminalRetryInitial
			continue
		}
		slog.Error("retry terminal static build task persistence failed", "task", taskID, "error", err)
		if delay < terminalRetryMaximum {
			delay *= 2
			if delay > terminalRetryMaximum {
				delay = terminalRetryMaximum
			}
		}
	}
}

func (s *Service) pruneTaskHistory() {
	if err := pruneBuildTasks(s.repository, retainedBuildTasks); err != nil {
		slog.Warn("prune stale static build tasks failed", "error", err)
	}
}

func pruneBuildTasks(repository *fsrepo.Repository, retain int) error {
	if retain < 1 {
		retain = 1
	}
	entries, err := repository.ReadDir(filepath.Join("state", "tasks"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	terminal := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var task Task
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return fmt.Errorf("read static build task directory entry %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return fmt.Errorf("static build task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "StaticBuild" {
			continue
		}
		if task.Status == "succeeded" || task.Status == "failed" {
			terminal = append(terminal, entry.Name())
		}
	}
	sort.Strings(terminal)
	for _, name := range terminal[:max(0, len(terminal)-retain)] {
		if err := repository.RemoveFile(filepath.Join("state", "tasks", name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func requireBuildOutputs(stagePath string) error {
	files := 0
	var bytes int64
	if err := filepath.WalkDir(stagePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("renderer output contains a symbolic link: %s", filepath.Base(path))
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("renderer output contains a non-regular file: %s", filepath.Base(path))
		}
		files++
		bytes += info.Size()
		if files > maxBuildFiles || bytes > maxBuildBytes {
			return errors.New("renderer output exceeds the release size limit")
		}
		return nil
	}); err != nil {
		return err
	}
	for _, name := range []string{"index.html", "redirects.json", "build-report.json"} {
		info, err := os.Stat(filepath.Join(stagePath, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("renderer output is missing %s", name)
		}
	}
	return nil
}

func newBuildID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

// NewBuildTaskID exposes the same collision-resistant ID format used by the
// publisher when an HTTP client did not supply a correlation ID. Callers that
// must return a queued task before Build starts can therefore persist and
// report the exact same durable record.
func NewBuildTaskID() (string, error) { return newBuildID() }

func newPreviewID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func validPreviewID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && id == strings.ToLower(id)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
