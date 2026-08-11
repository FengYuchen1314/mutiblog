package translation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type fakeRebuilder struct{ calls atomic.Int32 }

func (rebuilder *fakeRebuilder) Build(context.Context) (publisher.BuildReport, error) {
	rebuilder.calls.Add(1)
	return publisher.BuildReport{SchemaVersion: 1}, nil
}

// translationRoundTripFunc lets lifecycle tests observe the context handed to
// the provider request without coupling the assertion to a live TCP
// connection's shutdown timing.
type translationRoundTripFunc func(*http.Request) (*http.Response, error)

func (f translationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestTranslationTaskAppliesAIContentAndProtectsManualTranslation(t *testing.T) {
	var requests atomic.Int32
	var mutationDepth atomic.Int32
	var mutationAcquisitions atomic.Int32
	var providerCalledUnderMutationGate atomic.Bool
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if mutationDepth.Load() != 0 {
			providerCalledUnderMutationGate.Store(true)
		}
		var body struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		user := body.Messages[len(body.Messages)-1].Content
		response := "Translated " + user
		if strings.HasPrefix(user, "{") {
			response = `{"title":"Translated title","summary":"Translated summary","seoTitle":"","seoDescription":""}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": response}, "finish_reason": "stop"}}})
	}))
	defer providerServer.Close()

	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"zh-CN"}}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/providers.yaml", domain.AIProvidersConfig{SchemaVersion: 1, Providers: []domain.AIProviderConfig{}}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: 1, Providers: map[string]string{}}, true); err != nil {
		t.Fatal(err)
	}
	aiService := ai.NewService(repository, ai.Client{HTTPClient: providerServer.Client()})
	key := "fake-key"
	if _, err := aiService.Upsert("test", ai.UpsertProviderInput{Name: "Test", BaseURL: providerServer.URL, Model: "test", Enabled: true, Default: true, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "translation-post", Title: "源标题", Summary: "源摘要", Markdown: "正文 [链接](https://example.com)\n\n```go\nfmt.Println(1)\n```"})
	if err != nil {
		t.Fatal(err)
	}
	rebuilder := &fakeRebuilder{}
	service := NewService(repository, contentService, aiService, rebuilder)
	type contentChange struct {
		kind         string
		id           string
		revision     int
		mutationHeld bool
	}
	changes := make(chan contentChange, 2)
	service.SetContentMutationAcquire(func() func() {
		mutationAcquisitions.Add(1)
		mutationDepth.Add(1)
		return func() { mutationDepth.Add(-1) }
	})
	service.SetContentChangedCallback(func(kind, id string, revision int) {
		changes <- contentChange{kind: kind, id: id, revision: revision, mutationHeld: mutationDepth.Load() == 1}
	})
	task, err := service.Start(StartInput{PostID: post.Meta.ID, Locales: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	task = waitForTask(t, service, task.ID)
	if task.Status != "succeeded" || task.Progress.Percent != 100 || task.Progress.Current != task.Progress.Total || task.Targets[0].Status != "succeeded" || task.Targets[0].Progress.Percent != 100 || rebuilder.calls.Load() != 1 {
		t.Fatalf("task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
	}
	post, err = contentService.GetPost(post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case changed := <-changes:
		if changed.kind != "Post" || changed.id != post.Meta.ID || changed.revision != post.Meta.Revision || !changed.mutationHeld {
			t.Fatalf("content change callback = %#v, post revision = %d", changed, post.Meta.Revision)
		}
	default:
		t.Fatal("successful AI promotion did not notify the content change callback")
	}
	english := post.Content["en"]
	if post.Meta.Locales["en"].Origin != domain.LocaleOriginAI || english.Title != "Translated title" || !strings.Contains(english.Markdown, "https://example.com") || !strings.Contains(english.Markdown, "fmt.Println(1)") {
		t.Fatalf("translated post = %#v", post)
	}
	requestsBeforeRecovery := requests.Load()
	recoveryTask := Task{
		SchemaVersion: domain.SchemaVersion, ID: "translation-recovery", Kind: "Translation", EntityKind: "Post", EntityID: post.Meta.ID,
		SourceLocale: post.Meta.SourceLocale, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		ProviderID: "test", Model: "test", Status: "running", Targets: []TargetTask{{Locale: "en", Status: "running", Attempts: 1}}, CreatedAt: time.Now().UTC(),
	}
	initializeTranslationIdentity(&recoveryTask, post)
	recoveryTask.Targets[0].ResultContent = localizedContentFingerprint(post.Content["en"], true)
	if err := service.writeTask(recoveryTask); err != nil {
		t.Fatal(err)
	}
	if count, err := service.Recover(); err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	recoveryTask = waitForTask(t, service, recoveryTask.ID)
	if recoveryTask.Status != "succeeded" || recoveryTask.Targets[0].Status != "succeeded" || recoveryTask.Targets[0].Attempts != 1 || requests.Load() != requestsBeforeRecovery {
		t.Fatalf("recovered task = %#v, calls %d -> %d", recoveryTask, requestsBeforeRecovery, requests.Load())
	}
	select {
	case changed := <-changes:
		if changed.kind != "Post" || changed.id != post.Meta.ID || changed.revision != post.Meta.Revision || !changed.mutationHeld {
			t.Fatalf("recovered content change callback = %#v, post revision = %d", changed, post.Meta.Revision)
		}
	default:
		t.Fatal("recovered AI promotion did not notify the content change callback")
	}
	if providerCalledUnderMutationGate.Load() || mutationDepth.Load() != 0 || mutationAcquisitions.Load() != 2 {
		t.Fatalf("mutation hook: acquisitions=%d depth=%d providerUnderGate=%t", mutationAcquisitions.Load(), mutationDepth.Load(), providerCalledUnderMutationGate.Load())
	}
	post, err = contentService.UpdateLocale(post.Meta.ID, "en", content.UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "Manual", Markdown: "Manual"})
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	confirmation, err := service.Start(StartInput{PostID: post.Meta.ID, Locales: []string{"en"}})
	if !errors.Is(err, ErrManualConfirmation) || len(confirmation.Targets) != 1 || confirmation.Targets[0].Locale != "en" || requests.Load() != before {
		t.Fatalf("confirmation = %#v, err = %v, calls %d -> %d", confirmation, err, before, requests.Load())
	}
}

func TestSafeTaskErrorDoesNotExposeUnexpectedDetails(t *testing.T) {
	secret := errors.New("request failed for key sk-example at /private/path")
	if got := safeTaskError(secret); got != "translation-failed" {
		t.Fatalf("safeTaskError() = %q", got)
	}
}

func TestSafeTaskErrorReportsTargetTimeout(t *testing.T) {
	if got := safeTaskError(context.DeadlineExceeded); got != "translation-timeout" {
		t.Fatalf("safeTaskError(deadline) = %q", got)
	}
}

func TestStartWithoutTargetsDoesNotRequireProvider(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled:       []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}},
		Fallback:      []string{"en", "zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{Title: "Source", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, contentService, ai.NewService(repository, ai.Client{}), nil)
	if _, err := service.Start(StartInput{PostID: post.Meta.ID}); !errors.Is(err, ErrNoTargets) {
		t.Fatalf("Start() error = %v, want ErrNoTargets", err)
	}
}

func TestPartialTranslationBatchRebuildsSuccessfulTargets(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Messages) > 0 && strings.Contains(body.Messages[0].Content, " to de.") {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": `{"title":"English","summary":"","seoTitle":"","seoDescription":""}`}, "finish_reason": "stop"}}})
	}))
	defer providerServer.Close()

	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
			{Code: "de", Label: "Deutsch", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/providers.yaml", domain.AIProvidersConfig{SchemaVersion: domain.SchemaVersion}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}}, true); err != nil {
		t.Fatal(err)
	}
	aiService := ai.NewService(repository, ai.Client{HTTPClient: providerServer.Client()})
	key := "fake-key"
	if _, err := aiService.Upsert("test", ai.UpsertProviderInput{Name: "Test", BaseURL: providerServer.URL, Model: "test", Enabled: true, Default: true, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{Title: "源标题"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	rebuilder := &fakeRebuilder{}
	service := NewService(repository, contentService, aiService, rebuilder)
	task, err := service.Start(StartInput{PostID: post.Meta.ID, Locales: []string{"de", "en"}})
	if err != nil {
		t.Fatal(err)
	}
	task = waitForTask(t, service, task.ID)
	if task.Status != "failed" || task.Targets[0].Status != "failed" || task.Targets[1].Status != "succeeded" || rebuilder.calls.Load() != 1 {
		t.Fatalf("partial task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
	}
}

func TestCheckpointFailureAfterDurableTranslationStillRebuildsAndFinishes(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "checkpoint-post", Title: "源标题", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.ApplyAITranslation(post.Meta.ID, "en", content.ApplyAITranslationInput{
		ExpectedSourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		Content:                domain.LocalizedMarkdown{Title: "Translated", Markdown: "Body"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rebuilder := &fakeRebuilder{}
	service := NewService(repository, contentService, nil, rebuilder)
	defer service.Close()
	task := Task{
		SchemaVersion:  domain.SchemaVersion,
		ID:             "translation-checkpoint-resilience",
		Kind:           "Translation",
		EntityKind:     "Post",
		EntityID:       post.Meta.ID,
		SourceLocale:   post.Meta.SourceLocale,
		SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		Status:         "queued",
		Targets:        toTargetTasks([]string{"en"}, post.Meta.Locales),
		CreatedAt:      time.Now().UTC(),
	}
	initializeTranslationIdentity(&task, post)
	task.Targets[0].ResultContent = localizedContentFingerprint(post.Content["en"], true)
	initializeTranslationProgress(&task, post.Content[post.Meta.SourceLocale])
	if err := service.writeTask(task); err != nil {
		t.Fatal(err)
	}
	var committedCheckpointFailures atomic.Int32
	var terminalAttempts atomic.Int32
	service.writeTaskHook = func(candidate Task) error {
		if candidate.Status == "running" && candidate.Progress.Phase == "translation-target" && len(candidate.Targets) == 1 && candidate.Targets[0].Status == "succeeded" {
			if committedCheckpointFailures.Add(1) <= taskCheckpointWriteAttempts {
				return errors.New("injected committed checkpoint failure")
			}
		}
		if candidate.Status == "succeeded" {
			if terminalAttempts.Add(1) <= taskCheckpointWriteAttempts {
				return errors.New("injected terminal checkpoint failure")
			}
		}
		return nil
	}

	service.run(task.ID)
	stored := waitForTask(t, service, task.ID)
	if stored.Status != "succeeded" || stored.Progress.Percent != 100 || stored.Targets[0].Status != "succeeded" || rebuilder.calls.Load() != 1 || committedCheckpointFailures.Load() != taskCheckpointWriteAttempts || terminalAttempts.Load() != taskCheckpointWriteAttempts+1 {
		t.Fatalf("task = %#v, rebuilds = %d, committed checkpoint failures = %d, terminal attempts = %d", stored, rebuilder.calls.Load(), committedCheckpointFailures.Load(), terminalAttempts.Load())
	}
	released, err := contentService.GetPublishedRelease("Post", post.Meta.ID)
	if err != nil || released.Content["en"].Title != "Translated" {
		t.Fatalf("released translation = %#v, %v", released.Content["en"], err)
	}
}

func TestRecoverMarksLegacyTaskWithoutContentIdentityNeedsReview(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	defer service.Close()
	legacy := Task{
		SchemaVersion:  domain.SchemaVersion,
		ID:             "translation-legacy-recovery",
		Kind:           "Translation",
		EntityKind:     "Post",
		EntityID:       "restored-post",
		SourceLocale:   "zh-CN",
		SourceRevision: 1,
		Status:         "running",
		Targets:        []TargetTask{{Locale: "en", ExpectedRevision: 0, Status: "running"}},
		CreatedAt:      time.Now().UTC(),
	}
	if err := service.writeTask(legacy); err != nil {
		t.Fatal(err)
	}
	if count, err := service.Recover(); err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	stored, err := service.Get(legacy.ID)
	if err != nil || stored.Status != "needs-review" || stored.Error != "content-identity-unavailable" || stored.CompletedAt == nil {
		t.Fatalf("legacy recovered task = %#v, %v", stored, err)
	}
}

func TestCloseCancelsProviderAndWaitsForTranslationRunner(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	requestCanceled := make(chan struct{}, 1)
	providerClient := &http.Client{Transport: translationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		<-request.Context().Done()
		select {
		case requestCanceled <- struct{}{}:
		default:
		}
		return nil, request.Context().Err()
	})}
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/providers.yaml", domain.AIProvidersConfig{SchemaVersion: domain.SchemaVersion}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}}, true); err != nil {
		t.Fatal(err)
	}
	aiService := ai.NewService(repository, ai.Client{HTTPClient: providerClient})
	key := "fake-key"
	if _, err := aiService.Upsert("test", ai.UpsertProviderInput{Name: "Test", BaseURL: "https://provider.test", Model: "test", Enabled: true, Default: true, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "close-cancel-post", Title: "源标题", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, contentService, aiService, nil)
	t.Cleanup(service.Close)
	task, err := service.Start(StartInput{PostID: post.Meta.ID, Locales: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("translation provider request did not start")
	}
	closed := make(chan struct{})
	go func() {
		service.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel and wait for the provider request")
	}
	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Close returned before it canceled the provider request context")
	}
	stored, err := service.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "running" || stored.CompletedAt != nil {
		t.Fatalf("interrupted task should remain recoverable, got %#v", stored)
	}
}

func TestRestoreInvalidatesInflightPostAndPageTranslationsWithEqualRevisions(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			requestStarted := make(chan struct{})
			providerRelease := make(chan struct{})
			var requests atomic.Int32
			providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if requests.Add(1) == 1 {
					close(requestStarted)
					<-providerRelease
				}
				var body struct {
					Messages []ai.ChatMessage `json:"messages"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				response := "Translated restored-unsafe body"
				if len(body.Messages) > 0 && strings.HasPrefix(strings.TrimSpace(body.Messages[len(body.Messages)-1].Content), "{") {
					response = `{"title":"Translated old source","summary":"","seoTitle":"","seoDescription":""}`
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": response}, "finish_reason": "stop"}}})
			}))
			defer providerServer.Close()
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN",
				Enabled:  []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}},
				Fallback: []string{"zh-CN"},
			}, false); err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/providers.yaml", domain.AIProvidersConfig{SchemaVersion: domain.SchemaVersion}, false); err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}}, true); err != nil {
				t.Fatal(err)
			}
			aiService := ai.NewService(repository, ai.Client{HTTPClient: providerServer.Client()})
			key := "fake-key"
			if _, err := aiService.Upsert("test", ai.UpsertProviderInput{Name: "Test", BaseURL: providerServer.URL, Model: "test", Enabled: true, Default: true, APIKey: &key}); err != nil {
				t.Fatal(err)
			}
			contentService := content.NewService(repository)
			var item domain.Post
			if kind == "Page" {
				item, err = contentService.CreatePage(content.CreatePageInput{ID: "restore-page", Title: "Old source title", Markdown: "Old source body"})
			} else {
				item, err = contentService.CreatePost(content.CreatePostInput{ID: "restore-post", Title: "Old source title", Markdown: "Old source body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(repository, contentService, aiService, nil)
			defer service.Close()
			task, err := service.Start(StartInput{EntityKind: kind, PostID: item.Meta.ID, Locales: []string{"en"}})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-requestStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("provider request did not start")
			}
			resume := service.Pause()
			released := false
			defer func() {
				if !released {
					resume()
					close(providerRelease)
				}
			}()
			directory := "posts"
			if kind == "Page" {
				directory = "pages"
			}
			contentPath := filepath.Join("content", directory, item.Meta.ID, item.Meta.SourceLocale+".md")
			data, err := repository.ReadFile(contentPath)
			if err != nil {
				t.Fatal(err)
			}
			restored := strings.ReplaceAll(strings.ReplaceAll(string(data), "Old source title", "Restored source title"), "Old source body", "Restored source body")
			if restored == string(data) {
				t.Fatal("test fixture did not replace source content")
			}
			if err := repository.WriteFile(contentPath, []byte(restored), 0o640); err != nil {
				t.Fatal(err)
			}
			resume()
			close(providerRelease)
			released = true
			stored := waitForTask(t, service, task.ID)
			if stored.Status != "needs-review" {
				t.Fatalf("restored-content task = %#v", stored)
			}
			var current domain.Post
			if kind == "Page" {
				current, err = contentService.GetPage(item.Meta.ID)
			} else {
				current, err = contentService.GetPost(item.Meta.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if current.Meta.Locales[current.Meta.SourceLocale].Revision != item.Meta.Locales[item.Meta.SourceLocale].Revision || current.Content[current.Meta.SourceLocale].Title != "Restored source title" {
				t.Fatalf("restored source identity changed unexpectedly: %#v", current)
			}
			if _, exists := current.Content["en"]; exists {
				t.Fatalf("pre-restore translation crossed the restore boundary: %#v", current.Content["en"])
			}
		})
	}
}

func TestPersistedContentIdentitySurvivesRestartAfterRestore(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion,
				SourceLocale:  "zh-CN",
				Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}},
				Fallback:      []string{"zh-CN"},
			}, false); err != nil {
				t.Fatal(err)
			}
			contentService := content.NewService(repository)
			var item domain.Post
			if kind == "Page" {
				item, err = contentService.CreatePage(content.CreatePageInput{ID: "restart-restore-page", Title: "Old page", Markdown: "Old page body"})
			} else {
				item, err = contentService.CreatePost(content.CreatePostInput{ID: "restart-restore-post", Title: "Old post", Markdown: "Old post body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			beforeRestart := NewService(repository, contentService, nil, nil)
			task := Task{
				SchemaVersion:  domain.SchemaVersion,
				ID:             "translation-persisted-identity-" + strings.ToLower(kind),
				Kind:           "Translation",
				EntityKind:     kind,
				EntityID:       item.Meta.ID,
				SourceLocale:   item.Meta.SourceLocale,
				SourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision,
				Status:         "queued",
				Targets:        toTargetTasks([]string{"en"}, item.Meta.Locales),
				CreatedAt:      time.Now().UTC(),
			}
			initializeTranslationIdentity(&task, item)
			initializeTranslationProgress(&task, item.Content[item.Meta.SourceLocale])
			if err := beforeRestart.writeTask(task); err != nil {
				t.Fatal(err)
			}
			beforeRestart.Close()

			directory := "posts"
			if kind == "Page" {
				directory = "pages"
			}
			contentPath := filepath.Join("content", directory, item.Meta.ID, item.Meta.SourceLocale+".md")
			data, err := repository.ReadFile(contentPath)
			if err != nil {
				t.Fatal(err)
			}
			restored := strings.ReplaceAll(strings.ReplaceAll(string(data), "Old post", "Restored post"), "Old page", "Restored page")
			if restored == string(data) {
				t.Fatal("test fixture did not replace source content")
			}
			if err := repository.WriteFile(contentPath, []byte(restored), 0o640); err != nil {
				t.Fatal(err)
			}

			afterRestart := NewService(repository, contentService, nil, nil)
			defer afterRestart.Close()
			if count, err := afterRestart.Recover(); err != nil || count != 1 {
				t.Fatalf("Recover() = %d, %v", count, err)
			}
			stored := waitForTask(t, afterRestart, task.ID)
			if stored.Status != "needs-review" || stored.Error != "source-changed-before-start" {
				t.Fatalf("post-restart restored task = %#v", stored)
			}
		})
	}
}

func TestPersistedResultIdentityRejectsRestoredCompletedTarget(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion,
				SourceLocale:  "zh-CN",
				Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}},
				Fallback:      []string{"zh-CN"},
			}, false); err != nil {
				t.Fatal(err)
			}
			contentService := content.NewService(repository)
			var item domain.Post
			if kind == "Page" {
				item, err = contentService.CreatePage(content.CreatePageInput{ID: "result-restore-page", Title: "Source", Markdown: "Source body"})
			} else {
				item, err = contentService.CreatePost(content.CreatePostInput{ID: "result-restore-post", Title: "Source", Markdown: "Source body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			input := content.ApplyAITranslationInput{ExpectedSourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision, Content: domain.LocalizedMarkdown{Title: "Old translated", Markdown: "Old translated body"}}
			if kind == "Page" {
				item, err = contentService.ApplyAIPageTranslation(item.Meta.ID, "en", input)
			} else {
				item, err = contentService.ApplyAITranslation(item.Meta.ID, "en", input)
			}
			if err != nil {
				t.Fatal(err)
			}
			beforeRestart := NewService(repository, contentService, nil, nil)
			task := Task{
				SchemaVersion:  domain.SchemaVersion,
				ID:             "translation-persisted-result-" + strings.ToLower(kind),
				Kind:           "Translation",
				EntityKind:     kind,
				EntityID:       item.Meta.ID,
				SourceLocale:   item.Meta.SourceLocale,
				SourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision,
				Status:         "running",
				Targets:        toTargetTasks([]string{"en"}),
				CreatedAt:      time.Now().UTC(),
			}
			initializeTranslationIdentity(&task, item)
			// The simulated task began before the AI target existed, then durably
			// committed the result before its terminal checkpoint was stored.
			task.Targets[0].ExpectedContent = localizedContentFingerprint(domain.LocalizedMarkdown{}, false)
			task.Targets[0].ResultContent = localizedContentFingerprint(item.Content["en"], true)
			task.Targets[0].Status = "succeeded"
			initializeTranslationProgress(&task, item.Content[item.Meta.SourceLocale])
			if err := beforeRestart.writeTask(task); err != nil {
				t.Fatal(err)
			}
			beforeRestart.Close()

			directory := "posts"
			if kind == "Page" {
				directory = "pages"
			}
			targetPath := filepath.Join("content", directory, item.Meta.ID, "en.md")
			data, err := repository.ReadFile(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			restored := strings.ReplaceAll(string(data), "Old translated", "Restored translation")
			if restored == string(data) {
				t.Fatal("test fixture did not replace completed target content")
			}
			if err := repository.WriteFile(targetPath, []byte(restored), 0o640); err != nil {
				t.Fatal(err)
			}

			afterRestart := NewService(repository, contentService, nil, nil)
			defer afterRestart.Close()
			if count, err := afterRestart.Recover(); err != nil || count != 1 {
				t.Fatalf("Recover() = %d, %v", count, err)
			}
			stored := waitForTask(t, afterRestart, task.ID)
			if stored.Status != "needs-review" || stored.Targets[0].Status != "needs-review" {
				t.Fatalf("restored completed target task = %#v", stored)
			}
		})
	}
}

func TestListRejectsTaskWhoseIdentityDoesNotMatchItsPath(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	task := Task{
		SchemaVersion: domain.SchemaVersion,
		ID:            "translation-different",
		Kind:          "Translation",
		Status:        "queued",
		CreatedAt:     time.Now().UTC(),
	}
	if err := repository.WriteYAML("state/tasks/translation-expected.yaml", task, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(); err == nil {
		t.Fatal("expected invalid task identity to be reported")
	}
}

func TestListIgnoresValidBackupTaskInSharedTaskDirectory(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	if err := repository.WriteYAML("state/tasks/backup-task-example.yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion,
		"id":            "backup-task-example",
		"kind":          "Backup",
		"operation":     "create",
		"status":        "queued",
	}, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("translation tasks = %#v, %v", tasks, err)
	}
}

func TestListIgnoresValidStaticBuildTaskInSharedTaskDirectory(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	id := "20260811T010203.000000000Z-aabbccdd"
	if err := repository.WriteYAML("state/tasks/"+id+".yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion,
		"id":            id,
		"kind":          "StaticBuild",
		"status":        "succeeded",
	}, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("translation tasks = %#v, %v", tasks, err)
	}
}

func TestListIgnoresValidScheduledPublishTaskInSharedTaskDirectory(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	id := "scheduled-publish-20260811T010203.000000000Z-aabbccdd"
	if err := repository.WriteYAML("state/tasks/"+id+".yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion,
		"id":            id,
		"kind":          "ScheduledPublish",
		"status":        "queued",
	}, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("translation tasks = %#v, %v", tasks, err)
	}
}

func TestListIgnoresValidIndexRebuildTaskInSharedTaskDirectory(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	id := "index-rebuild-20260811T010203.000000000Z-aabbccdd"
	if err := repository.WriteYAML("state/tasks/"+id+".yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion,
		"id":            id,
		"kind":          "IndexRebuild",
		"status":        "queued",
	}, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("translation tasks = %#v, %v", tasks, err)
	}
}

func TestListRejectsUnknownSharedTaskKind(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, nil, nil)
	if err := repository.WriteYAML("state/tasks/unknown.yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion,
		"id":            "unknown",
		"kind":          "Unknown",
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(); err == nil {
		t.Fatal("expected an unknown shared task kind to be reported")
	}
}

func TestSameTargetsRequiresTheSameOrderedLocaleSet(t *testing.T) {
	existing := []TargetTask{{Locale: "de"}, {Locale: "en"}}
	if !sameTargets(existing, []string{"de", "en"}) {
		t.Fatal("identical target sets should match")
	}
	if sameTargets(existing, []string{"en", "de"}) || sameTargets(existing, []string{"de"}) {
		t.Fatal("different target sets must not match")
	}
}

func TestTranslationTaskIDRejectsPaths(t *testing.T) {
	for _, id := range []string{"../translation-task", "translation-../../config", "/translation-task", "other-task"} {
		if validTaskID(id) {
			t.Fatalf("unsafe translation task ID accepted: %q", id)
		}
	}
	if !validTaskID("translation-recovery") {
		t.Fatal("normal translation task ID was rejected")
	}
}

func waitForTask(t *testing.T, service *Service, id string) Task {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task, err := service.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == "succeeded" || task.Status == "failed" || task.Status == "needs-review" {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("translation task timed out")
	return Task{}
}
