package translation

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestReconcilePublishedPublicationsRestoresPendingGenerationOnce(t *testing.T) {
	providerStarted := make(chan struct{})
	providerRelease := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	var requests atomic.Int32
	releaseProvider := func() { releaseOnce.Do(func() { close(providerRelease) }) }
	defer releaseProvider()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		if requestNumber == 1 {
			startedOnce.Do(func() { close(providerStarted) })
			<-providerRelease
		}
		var body struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		response := "Translated body"
		if len(body.Messages) > 0 && strings.HasPrefix(strings.TrimSpace(body.Messages[len(body.Messages)-1].Content), "{") {
			response = `{"title":"Translated title","summary":"Translated summary","seoTitle":"","seoDescription":""}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": response}, "finish_reason": "stop"}},
		})
	})
	service, post := newRunnableTranslationFixture(t, handler)
	published, err := service.content.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := published.Meta.Locales["en"]; exists {
		t.Fatal("fixture must cover a first publication whose enabled target is absent")
	}

	if count, err := service.ReconcilePublishedPublications(); err != nil || count != 1 {
		t.Fatalf("ReconcilePublishedPublications() = %d, %v", count, err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("recovered tasks = %#v, %v", tasks, err)
	}
	if tasks[0].Status != "queued" || tasks[0].PublicationGeneration != published.Meta.PublicationGeneration || tasks[0].PublicationRevision < 1 || !tasks[0].OverwriteManual {
		t.Fatalf("recovered publication identity = %#v, release = %#v", tasks[0], published.Meta)
	}

	// The second pass sees the same stale release, but Prepare must reuse the
	// durable task for this exact generation instead of creating a second receipt.
	if count, err := service.ReconcilePublishedPublications(); err != nil || count != 1 {
		t.Fatalf("idempotent ReconcilePublishedPublications() = %d, %v", count, err)
	}
	tasks, err = service.List()
	if err != nil || len(tasks) != 1 || requests.Load() != 0 {
		t.Fatalf("idempotent recovered tasks = %#v, requests=%d, err=%v", tasks, requests.Load(), err)
	}
	if count, err := service.Recover(); err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	select {
	case <-providerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("recovered publication task did not launch")
	}

	releaseProvider()
	completed := waitForTask(t, service, tasks[0].ID)
	if completed.Status != "succeeded" {
		t.Fatalf("recovered task = %#v", completed)
	}
	released, err := service.content.GetPublishedRelease("Post", post.Meta.ID)
	if err != nil || released.Meta.PublicationGeneration != published.Meta.PublicationGeneration || released.Meta.Locales["en"].State != "current" {
		t.Fatalf("recovered release = %#v, %v", released, err)
	}
	if count, err := service.ReconcilePublishedPublications(); err != nil || count != 0 {
		t.Fatalf("current ReconcilePublishedPublications() = %d, %v", count, err)
	}
}

func TestPendingPublicationTargetsSelectsOnlyExplicitRecoveryTargets(t *testing.T) {
	service, _ := newRunnableTranslationFixture(t, successfulTranslationHandler(t, nil))
	if err := service.repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
			{Code: "de", Label: "Deutsch", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
			{Code: "fr", Label: "Français", Enabled: true},
			{Code: "ja", Label: "日本語", Enabled: true},
			{Code: "ko", Label: "한국어", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	released := domain.Post{
		Meta: domain.PostMeta{
			SourceLocale: "zh-CN",
			Locales: map[string]domain.LocaleContentState{
				"zh-CN": {State: "current", Origin: domain.LocaleOriginSource, Revision: 3, SourceRevision: 3},
				"en":    {State: "current", Origin: domain.LocaleOriginManual, Revision: 1, SourceRevision: 3},
				"fr":    {State: "stale", Origin: domain.LocaleOriginManual, Revision: 1, SourceRevision: 3},
				"ja":    {State: "current", Origin: domain.LocaleOriginAI, Revision: 1, SourceRevision: 2},
				"ko":    {State: "current", Origin: domain.LocaleOriginAI, Revision: 1, SourceRevision: 3},
			},
		},
		Content: map[string]domain.LocalizedMarkdown{
			"zh-CN": {Title: "源标题", Markdown: "源正文"},
			"en":    {Title: "Manual", Markdown: "Manual"},
			"fr":    {Title: "Manual stale", Markdown: "Manual stale"},
			"ja":    {Title: "Old AI", Markdown: "Old AI"},
			"ko":    {Title: "Incomplete AI"},
		},
	}
	pending, err := service.pendingPublicationTargets(released)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"de", "fr", "ja", "ko"}; !reflect.DeepEqual(pending, want) {
		t.Fatalf("pendingPublicationTargets() = %v, want %v", pending, want)
	}
}

func TestReconcilePublishedPublicationsDoesNotReuseOlderPublicationGeneration(t *testing.T) {
	service, post := newRunnableTranslationFixture(t, successfulTranslationHandler(t, nil))
	firstRelease, err := service.content.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := service.Prepare(StartInput{
		EntityKind: "Post", PostID: post.Meta.ID, OverwriteManual: true,
		PublishedRelease: true, PublicationGeneration: firstRelease.Meta.PublicationGeneration,
		SkipCurrentAI: true, RecordPreflightFailure: true,
	})
	if err != nil || !created {
		t.Fatalf("first Prepare() = %#v, %t, %v", first, created, err)
	}
	secondRelease, err := service.content.PublishPost(post.Meta.ID, firstRelease.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if secondRelease.Meta.PublicationGeneration == first.PublicationGeneration {
		t.Fatalf("same-source republish reused generation: first=%#v second=%#v", first, secondRelease.Meta)
	}

	if count, err := service.ReconcilePublishedPublications(); err != nil || count != 1 {
		t.Fatalf("ReconcilePublishedPublications() = %d, %v", count, err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 2 {
		t.Fatalf("publication tasks = %#v, %v", tasks, err)
	}
	var current Task
	for _, task := range tasks {
		if task.ID != first.ID {
			current = task
		}
	}
	if current.ID == "" || current.PublicationGeneration != secondRelease.Meta.PublicationGeneration || current.PublicationGeneration == first.PublicationGeneration {
		t.Fatalf("replacement publication task = %#v; first=%#v second=%#v", current, first, secondRelease.Meta)
	}
}

func TestReconcilePublishedPublicationsDoesNotRetryTerminalPublicationReceipt(t *testing.T) {
	service, post := newRunnableTranslationFixture(t, successfulTranslationHandler(t, nil))
	published, err := service.content.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	task, created, err := service.Prepare(StartInput{
		EntityKind: "Post", PostID: post.Meta.ID, OverwriteManual: true,
		PublishedRelease: true, PublicationGeneration: published.Meta.PublicationGeneration,
		SkipCurrentAI: true, RecordPreflightFailure: true,
	})
	if err != nil || !created {
		t.Fatalf("Prepare() = %#v, %t, %v", task, created, err)
	}
	now := time.Now().UTC()
	task.Status = "needs-review"
	task.CompletedAt = &now
	task.Error = "target-needs-review"
	if err := service.writeTask(task); err != nil {
		t.Fatal(err)
	}

	if count, err := service.ReconcilePublishedPublications(); err != nil || count != 1 {
		t.Fatalf("ReconcilePublishedPublications() = %d, %v", count, err)
	}
	tasks, err := service.List()
	if err != nil || len(tasks) != 1 || tasks[0].ID != task.ID || tasks[0].Status != "needs-review" || tasks[0].PublicationGeneration != published.Meta.PublicationGeneration {
		t.Fatalf("terminal publication receipt was retried: %#v, %v", tasks, err)
	}
}
