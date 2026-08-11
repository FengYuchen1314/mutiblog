package translation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestTranslationTaskAppliesAIContentAndProtectsManualTranslation(t *testing.T) {
	var requests atomic.Int32
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
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
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": response}}}})
	}))
	defer providerServer.Close()

	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"en", "zh-CN"}}
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
	task, err := service.Start(StartInput{PostID: post.Meta.ID, Locales: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	task = waitForTask(t, service, task.ID)
	if task.Status != "succeeded" || task.Targets[0].Status != "succeeded" || rebuilder.calls.Load() != 1 {
		t.Fatalf("task = %#v, rebuilds = %d", task, rebuilder.calls.Load())
	}
	post, err = contentService.GetPost(post.Meta.ID)
	if err != nil {
		t.Fatal(err)
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
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": `{"title":"English","summary":"","seoTitle":"","seoDescription":""}`}}}})
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
		Fallback: []string{"en", "zh-CN"},
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
