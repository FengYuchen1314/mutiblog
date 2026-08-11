package translation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"golang.org/x/text/language"
)

var (
	ErrNoTargets          = errors.New("no translation targets")
	ErrManualConfirmation = errors.New("manual translations require confirmation")
	ErrTaskNotFound       = errors.New("translation task not found")
)

const translationTargetTimeout = 15 * time.Minute

type Rebuilder interface {
	Build(context.Context) (publisher.BuildReport, error)
}

type TargetTask struct {
	Locale           string     `yaml:"locale" json:"locale"`
	ExpectedRevision int        `yaml:"expectedRevision,omitempty" json:"expectedRevision"`
	Status           string     `yaml:"status" json:"status"`
	Attempts         int        `yaml:"attempts" json:"attempts"`
	Error            string     `yaml:"error,omitempty" json:"error,omitempty"`
	CompletedAt      *time.Time `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
}

type Task struct {
	SchemaVersion   int          `yaml:"schemaVersion" json:"schemaVersion"`
	ID              string       `yaml:"id" json:"id"`
	Kind            string       `yaml:"kind" json:"kind"`
	EntityKind      string       `yaml:"entityKind" json:"entityKind"`
	EntityID        string       `yaml:"entityId" json:"entityId"`
	SourceLocale    string       `yaml:"sourceLocale" json:"sourceLocale"`
	SourceRevision  int          `yaml:"sourceRevision" json:"sourceRevision"`
	ProviderID      string       `yaml:"providerId" json:"providerId"`
	Model           string       `yaml:"model" json:"model"`
	OverwriteManual bool         `yaml:"overwriteManual" json:"overwriteManual"`
	Status          string       `yaml:"status" json:"status"`
	Targets         []TargetTask `yaml:"targets" json:"targets"`
	CreatedAt       time.Time    `yaml:"createdAt" json:"createdAt"`
	StartedAt       *time.Time   `yaml:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt     *time.Time   `yaml:"completedAt,omitempty" json:"completedAt,omitempty"`
	Error           string       `yaml:"error,omitempty" json:"error,omitempty"`
}

type StartInput struct {
	EntityKind      string
	PostID          string
	Locales         []string
	OverwriteManual bool
	SkipManual      bool
}

type Service struct {
	repository *fsrepo.Repository
	content    *content.Service
	ai         *ai.Service
	rebuilder  Rebuilder
	semaphore  chan struct{}
	startMu    sync.Mutex
	mu         sync.Mutex
	runGate    sync.RWMutex
}

func NewService(repository *fsrepo.Repository, contentService *content.Service, aiService *ai.Service, rebuilder Rebuilder) *Service {
	return &Service{repository: repository, content: contentService, ai: aiService, rebuilder: rebuilder, semaphore: make(chan struct{}, 1)}
}

// Recover requeues translation work that was interrupted by a process stop.
// Already-applied AI translations are detected from their source revision, so
// recovery does not spend provider quota translating the same revision twice.
func (s *Service) Recover() (int, error) {
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		task.Status = "queued"
		task.StartedAt = nil
		task.CompletedAt = nil
		task.Error = ""
		for index := range task.Targets {
			if task.Targets[index].Status == "running" {
				task.Targets[index].Status = "queued"
				task.Targets[index].CompletedAt = nil
				task.Targets[index].Error = ""
			}
		}
		if err := s.writeTask(task); err != nil {
			return recovered, err
		}
		recovered++
		go s.run(task.ID)
	}
	return recovered, nil
}

func (s *Service) Start(input StartInput) (Task, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	entityKind := input.EntityKind
	if entityKind == "" {
		entityKind = "Post"
	}
	post, err := s.getContent(entityKind, input.PostID)
	if err != nil {
		return Task{}, err
	}
	targets, manual, err := s.targets(post, input.Locales)
	if err != nil {
		return Task{}, err
	}
	if len(manual) > 0 && !input.OverwriteManual {
		if input.SkipManual {
			manualSet := make(map[string]bool, len(manual))
			for _, locale := range manual {
				manualSet[locale] = true
			}
			filtered := targets[:0]
			for _, locale := range targets {
				if !manualSet[locale] {
					filtered = append(filtered, locale)
				}
			}
			targets = filtered
			if len(targets) == 0 {
				return Task{}, ErrNoTargets
			}
		} else {
			return Task{Targets: toTargetTasks(manual)}, ErrManualConfirmation
		}
	}
	provider, _, err := s.ai.DefaultCredentials()
	if err != nil {
		return Task{}, err
	}
	if existing, ok, err := s.activeTask(entityKind, post.Meta.ID, post.Meta.SourceLocale, post.Meta.Locales[post.Meta.SourceLocale].Revision, provider.ID, input.OverwriteManual, targets); err != nil {
		return Task{}, err
	} else if ok {
		return existing, nil
	}
	taskID, err := newTaskID()
	if err != nil {
		return Task{}, err
	}
	task := Task{
		SchemaVersion: domain.SchemaVersion, ID: taskID, Kind: "Translation", EntityKind: entityKind, EntityID: post.Meta.ID,
		SourceLocale: post.Meta.SourceLocale, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		ProviderID: provider.ID, Model: provider.Model, OverwriteManual: input.OverwriteManual,
		Status: "queued", Targets: toTargetTasks(targets, post.Meta.Locales), CreatedAt: time.Now().UTC(),
	}
	if err := s.writeTask(task); err != nil {
		return Task{}, err
	}
	go s.run(task.ID)
	return task, nil
}

// activeTask prevents duplicate clicks, repeated publish requests, and client
// retries from spending provider quota on identical in-flight work. Completed
// tasks are deliberately excluded so an administrator can explicitly run the
// same translation again after reviewing the result.
func (s *Service) activeTask(entityKind, entityID, sourceLocale string, sourceRevision int, providerID string, overwriteManual bool, targets []string) (Task, bool, error) {
	tasks, err := s.List()
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "running" {
			continue
		}
		if task.EntityKind != entityKind || task.EntityID != entityID || task.SourceLocale != sourceLocale || task.SourceRevision != sourceRevision || task.ProviderID != providerID || task.OverwriteManual != overwriteManual {
			continue
		}
		if sameTargets(task.Targets, targets) {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func sameTargets(existing []TargetTask, requested []string) bool {
	if len(existing) != len(requested) {
		return false
	}
	for index := range existing {
		if existing[index].Locale != requested[index] {
			return false
		}
	}
	return true
}

func (s *Service) List() ([]Task, error) {
	entries, err := s.repository.ReadDir(filepath.Join("state", "tasks"))
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var task Task
		expectedID := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := s.repository.ReadYAML(filepath.Join("state", "tasks", entry.Name()), &task); err != nil {
			return nil, fmt.Errorf("read translation task %q: %w", expectedID, err)
		}
		if err := taskstore.Validate(taskstore.Header{SchemaVersion: task.SchemaVersion, ID: task.ID, Kind: task.Kind}, expectedID); err != nil {
			return nil, fmt.Errorf("translation task directory entry %q is invalid: %w", expectedID, err)
		}
		if task.Kind != "Translation" {
			continue
		}
		if !validStoredTask(task, expectedID) {
			return nil, fmt.Errorf("translation task %q is invalid", expectedID)
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks, nil
}

func (s *Service) Get(id string) (Task, error) {
	if !validTaskID(id) {
		return Task{}, ErrTaskNotFound
	}
	var task Task
	if err := s.repository.ReadYAML(filepath.Join("state", "tasks", id+".yaml"), &task); err != nil {
		return Task{}, ErrTaskNotFound
	}
	if !validStoredTask(task, id) {
		return Task{}, ErrTaskNotFound
	}
	return task, nil
}

func (s *Service) targets(post domain.Post, requested []string) ([]string, []string, error) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return nil, nil, err
	}
	enabled := make(map[string]bool)
	for _, locale := range config.Enabled {
		if locale.Enabled && locale.Code != post.Meta.SourceLocale {
			enabled[locale.Code] = true
		}
	}
	if len(requested) == 0 {
		for locale := range enabled {
			requested = append(requested, locale)
		}
	}
	unique := make(map[string]bool)
	var targets, manual []string
	for _, raw := range requested {
		tag, err := language.Parse(raw)
		if err != nil || !enabled[tag.String()] {
			return nil, nil, content.ErrLocaleDisabled
		}
		locale := tag.String()
		if unique[locale] {
			continue
		}
		unique[locale] = true
		targets = append(targets, locale)
		if state, exists := post.Meta.Locales[locale]; exists && state.Origin == domain.LocaleOriginManual {
			manual = append(manual, locale)
		}
	}
	if len(targets) == 0 {
		return nil, nil, ErrNoTargets
	}
	sort.Strings(targets)
	sort.Strings(manual)
	return targets, manual, nil
}

func (s *Service) run(taskID string) {
	s.runGate.RLock()
	defer s.runGate.RUnlock()
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()
	task, err := s.Get(taskID)
	if err != nil {
		return
	}
	started := time.Now().UTC()
	task.Status = "running"
	task.StartedAt = &started
	if err := s.writeTask(task); err != nil {
		return
	}
	post, err := s.getContent(task.EntityKind, task.EntityID)
	if err != nil || post.Meta.Locales[post.Meta.SourceLocale].Revision != task.SourceRevision {
		s.finishTask(&task, "needs-review", "source-changed-before-start")
		return
	}
	source := post.Content[post.Meta.SourceLocale]
	failed := false
	needsReview := false
	hasSuccessfulTarget := false
	for index := range task.Targets {
		if task.Targets[index].Status == "succeeded" {
			hasSuccessfulTarget = true
			continue
		}
		post, postErr := s.getContent(task.EntityKind, task.EntityID)
		if postErr != nil {
			failed = true
			completed := time.Now().UTC()
			task.Targets[index].Status = "failed"
			task.Targets[index].Error = safeTaskError(postErr)
			task.Targets[index].CompletedAt = &completed
			if err := s.writeTask(task); err != nil {
				return
			}
			continue
		}
		if state, exists := post.Meta.Locales[task.Targets[index].Locale]; exists && state.Origin == domain.LocaleOriginAI && state.State == "current" && state.SourceRevision == task.SourceRevision {
			if promoteErr := s.content.PromoteAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, task.SourceRevision); promoteErr != nil {
				failed = true
				task.Targets[index].Status = taskStatusForError(promoteErr)
				needsReview = needsReview || task.Targets[index].Status == "needs-review"
				task.Targets[index].Error = safeTaskError(promoteErr)
				if err := s.writeTask(task); err != nil {
					return
				}
				continue
			}
			completed := time.Now().UTC()
			task.Targets[index].Status = "succeeded"
			hasSuccessfulTarget = true
			task.Targets[index].Error = ""
			task.Targets[index].CompletedAt = &completed
			if err := s.writeTask(task); err != nil {
				return
			}
			continue
		}
		currentRevision := 0
		if state, exists := post.Meta.Locales[task.Targets[index].Locale]; exists {
			currentRevision = state.Revision
		}
		if currentRevision != task.Targets[index].ExpectedRevision {
			failed = true
			needsReview = true
			completed := time.Now().UTC()
			task.Targets[index].Status = "needs-review"
			task.Targets[index].Error = safeTaskError(content.ErrTargetChanged)
			task.Targets[index].CompletedAt = &completed
			if err := s.writeTask(task); err != nil {
				return
			}
			continue
		}
		task.Targets[index].Status = "running"
		task.Targets[index].Attempts++
		if err := s.writeTask(task); err != nil {
			return
		}
		targetContext, cancelTarget := context.WithTimeout(context.Background(), translationTargetTimeout)
		translated, translateErr := s.translate(targetContext, task.ProviderID, task.SourceLocale, task.Targets[index].Locale, source)
		cancelTarget()
		if translateErr == nil {
			expectedTargetRevision := task.Targets[index].ExpectedRevision
			_, translateErr = s.applyAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, content.ApplyAITranslationInput{
				ExpectedSourceRevision: task.SourceRevision, ExpectedTargetRevision: &expectedTargetRevision, OverwriteManual: task.OverwriteManual, Content: translated,
			})
			if translateErr == nil {
				translateErr = s.content.PromoteAITranslation(task.EntityKind, task.EntityID, task.Targets[index].Locale, task.SourceRevision)
			}
		}
		completed := time.Now().UTC()
		task.Targets[index].CompletedAt = &completed
		if translateErr != nil {
			failed = true
			task.Targets[index].Status = taskStatusForError(translateErr)
			needsReview = needsReview || task.Targets[index].Status == "needs-review"
			task.Targets[index].Error = safeTaskError(translateErr)
		} else {
			task.Targets[index].Status = "succeeded"
			hasSuccessfulTarget = true
		}
		if err := s.writeTask(task); err != nil {
			return
		}
	}
	// A mixed batch may have already promoted verified locales into the public
	// content release. Rebuild once for those successful targets even when a
	// sibling target failed or needs review.
	if hasSuccessfulTarget && s.rebuilder != nil {
		if _, err := s.rebuilder.Build(context.Background()); err != nil {
			s.finishTask(&task, "failed", "rebuild-failed")
			return
		}
	}
	if needsReview {
		s.finishTask(&task, "needs-review", "target-needs-review")
		return
	}
	if failed {
		s.finishTask(&task, "failed", "target-failed")
		return
	}
	s.finishTask(&task, "succeeded", "")
}

// Pause waits for in-flight translation work and prevents queued tasks from
// mutating content until the returned resume function is called.
func (s *Service) Pause() func() {
	s.runGate.Lock()
	return s.runGate.Unlock
}

func (s *Service) getContent(kind, id string) (domain.Post, error) {
	switch kind {
	case "Post":
		return s.content.GetPost(id)
	case "Page":
		return s.content.GetPage(id)
	default:
		return domain.Post{}, content.ErrNotFound
	}
}

func (s *Service) applyAITranslation(kind, id, locale string, input content.ApplyAITranslationInput) (domain.Post, error) {
	switch kind {
	case "Post":
		return s.content.ApplyAITranslation(id, locale, input)
	case "Page":
		return s.content.ApplyAIPageTranslation(id, locale, input)
	default:
		return domain.Post{}, content.ErrNotFound
	}
}

func (s *Service) finishTask(task *Task, status, message string) {
	completed := time.Now().UTC()
	task.Status = status
	task.Error = message
	task.CompletedAt = &completed
	_ = s.writeTask(*task)
}

func (s *Service) writeTask(task Task) error {
	if !validTaskID(task.ID) {
		return ErrTaskNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repository.WriteYAML(filepath.Join("state", "tasks", task.ID+".yaml"), task, false)
}

func validTaskID(id string) bool {
	return strings.HasPrefix(id, "translation-") && len(id) < 128 && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

func validStoredTask(task Task, expectedID string) bool {
	return task.SchemaVersion == domain.SchemaVersion && task.Kind == "Translation" && task.ID == expectedID && validTaskID(task.ID)
}

func toTargetTasks(locales []string, states ...map[string]domain.LocaleContentState) []TargetTask {
	targets := make([]TargetTask, 0, len(locales))
	for _, locale := range locales {
		expectedRevision := 0
		if len(states) > 0 {
			expectedRevision = states[0][locale].Revision
		}
		targets = append(targets, TargetTask{Locale: locale, ExpectedRevision: expectedRevision, Status: "queued"})
	}
	return targets
}

func taskStatusForError(err error) string {
	if errors.Is(err, content.ErrSourceChanged) || errors.Is(err, content.ErrTargetChanged) || errors.Is(err, content.ErrManualProtected) {
		return "needs-review"
	}
	return "failed"
}

func safeTaskError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "translation-timeout"
	case errors.Is(err, content.ErrSourceChanged):
		return "source-changed"
	case errors.Is(err, content.ErrTargetChanged):
		return "target-changed"
	case errors.Is(err, content.ErrManualProtected):
		return "manual-protected"
	case errors.Is(err, ai.ErrKeyMissing):
		return "provider-key-missing"
	case errors.Is(err, ai.ErrProviderFailed):
		return "provider-request-failed"
	case errors.Is(err, ai.ErrInvalidProvider), errors.Is(err, ai.ErrProviderNotFound):
		return "provider-unavailable"
	case errors.Is(err, ErrUnsafeOutput):
		return "unsafe-output"
	default:
		return "translation-failed"
	}
}

func newTaskID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "translation-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

func randomTokenPrefix() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}
