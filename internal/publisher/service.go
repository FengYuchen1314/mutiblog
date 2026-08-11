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
	Locales      map[string]LocalizedTaxonomyInput `json:"locales"`
}

type ThemeInput struct {
	ID            string                         `json:"id"`
	ModulePath    string                         `json:"modulePath,omitempty"`
	AssetsPath    string                         `json:"assetsPath,omitempty"`
	Settings      map[string]any                 `json:"settings"`
	PostTemplates []themeservice.ContentTemplate `json:"postTemplates,omitempty"`
	PageTemplates []themeservice.ContentTemplate `json:"pageTemplates,omitempty"`
}

type BuildInput struct {
	SchemaVersion int                       `json:"schemaVersion"`
	SourceLocale  string                    `json:"sourceLocale"`
	BaseURL       string                    `json:"baseUrl,omitempty"`
	PrimaryMenu   string                    `json:"primaryMenu,omitempty"`
	Locales       []domain.LocaleDefinition `json:"locales"`
	Site          struct {
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

type Service struct {
	repository *fsrepo.Repository
	content    *content.Service
	renderer   Renderer
	mu         sync.Mutex
}

const (
	retainedStaticReleases = 20
	retainedBuildTasks     = 200
	retainedThemePreviews  = 10
	rendererTimeout        = 2 * time.Minute
	themePreviewTTL        = 30 * time.Minute
	maxBuildFiles          = 100000
	maxBuildBytes          = int64(1 << 30)
)

type Task struct {
	SchemaVersion int          `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string       `yaml:"id" json:"id"`
	Kind          string       `yaml:"kind" json:"kind"`
	Status        string       `yaml:"status" json:"status"`
	StartedAt     time.Time    `yaml:"startedAt" json:"startedAt"`
	CompletedAt   *time.Time   `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Report        *BuildReport `yaml:"report,omitempty" json:"report,omitempty"`
	Error         string       `yaml:"error,omitempty" json:"error,omitempty"`
}

func NewService(repository *fsrepo.Repository, contentService *content.Service, renderer Renderer) *Service {
	return &Service{repository: repository, content: contentService, renderer: renderer}
}

func (s *Service) Recover() (int, error) {
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
		if task.Kind != "StaticBuild" || task.Status != "running" {
			continue
		}
		completed := time.Now().UTC()
		task.Status = "failed"
		task.CompletedAt = &completed
		task.Error = "interrupted"
		if err := s.repository.WriteYAML(path, task, false); err != nil {
			return recovered, err
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

func (s *Service) Build(ctx context.Context) (BuildReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.renderer == nil {
		return BuildReport{}, errors.New("publisher renderer is not configured")
	}
	taskID, err := newBuildID()
	if err != nil {
		return BuildReport{}, err
	}
	task := Task{SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "StaticBuild", Status: "running", StartedAt: time.Now().UTC()}
	if err := s.writeTask(task); err != nil {
		return BuildReport{}, err
	}
	input, err := s.snapshot("")
	if err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	inputData, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	generatedRoot := filepath.Join(s.repository.Root(), "generated")
	inputPath := filepath.Join(generatedRoot, "staging", taskID+".input.json")
	stagePath := filepath.Join(generatedRoot, "staging", taskID)
	if err := os.WriteFile(inputPath, inputData, 0o640); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	defer os.Remove(inputPath)
	defer os.RemoveAll(stagePath)

	renderContext, cancelRender := context.WithTimeout(ctx, rendererTimeout)
	defer cancelRender()
	report, err := s.renderer.Render(renderContext, inputPath, stagePath)
	if err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if report.SchemaVersion != domain.SchemaVersion {
		return BuildReport{}, s.failTask(task, errors.New("renderer returned an unsupported report schema"))
	}
	if err := requireBuildOutputs(stagePath); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	if err := s.activate(taskID, stagePath); err != nil {
		return BuildReport{}, s.failTask(task, err)
	}
	completed := time.Now().UTC()
	task.Status = "succeeded"
	task.CompletedAt = &completed
	task.Report = &report
	if err := s.writeTask(task); err != nil {
		return BuildReport{}, err
	}
	s.pruneTaskHistory()
	return report, nil
}

func (s *Service) Preview(ctx context.Context, themeID, baseURL string) (PreviewRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	posts, err := s.content.ListPostsForBuild()
	if err != nil {
		return BuildInput{}, err
	}
	input := BuildInput{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  locales.SourceLocale,
		BaseURL:       site.BaseURL,
		PrimaryMenu:   site.PrimaryMenu,
		Fallback:      append([]string(nil), locales.Fallback...),
		GeneratedAt:   time.Now().UTC(),
		Posts:         make([]PostInput, 0, len(posts)),
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
		ID:            themeRuntime.ID,
		ModulePath:    themeRuntime.ModulePath,
		AssetsPath:    themeRuntime.AssetsPath,
		Settings:      themeRuntime.Settings,
		PostTemplates: themeRuntime.PostTemplates,
		PageTemplates: themeRuntime.PageTemplates,
	}
	for _, locale := range locales.Enabled {
		if locale.Enabled {
			input.Locales = append(input.Locales, locale)
		}
	}
	input.Site.Locales = site.Locales
	for _, post := range posts {
		localized := make(map[string]LocalizedPost, len(post.Content))
		for locale, value := range post.Content {
			localized[locale] = LocalizedPost{Title: value.Title, Summary: value.Summary, SEOTitle: value.SEOTitle, SEODescription: value.SEODescription, Markdown: value.Markdown}
		}
		input.Posts = append(input.Posts, PostInput{ID: post.Meta.ID, SourceLocale: post.Meta.SourceLocale, Status: post.Meta.Status, Template: post.Meta.Template, Cover: post.Meta.Cover, PublishedAt: post.Meta.PublishedAt, Categories: append([]string(nil), post.Meta.Categories...), Tags: append([]string(nil), post.Meta.Tags...), CommentPolicy: post.Meta.CommentPolicy, Locales: localized})
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
	sort.Slice(input.Posts, func(i, j int) bool { return input.Posts[i].ID < input.Posts[j].ID })
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
		result = append(result, TaxonomyInput{ID: item.ID, SourceLocale: item.SourceLocale, ParentID: item.ParentID, Locales: localized})
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
		return err
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
	if err := s.writeTask(task); err != nil {
		return fmt.Errorf("%v; record task: %w", buildErr, err)
	}
	s.pruneTaskHistory()
	return buildErr
}

func (s *Service) writeTask(task Task) error {
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
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
		if task.Status != "running" {
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
