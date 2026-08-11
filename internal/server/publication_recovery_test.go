package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type startupReceiptPublisher struct {
	repository *fsrepo.Repository
	seen       chan int
}

func (p *startupReceiptPublisher) Build(context.Context) (publisher.BuildReport, error) {
	entries, err := p.repository.ReadDir("state/tasks")
	count := -1
	if err == nil {
		count = 0
		for _, entry := range entries {
			if !entry.IsDir() {
				count++
			}
		}
	}
	// The test only needs the first startup build observation. Avoid letting an
	// unexpected retry make the fake publisher itself block server shutdown.
	select {
	case p.seen <- count:
	default:
	}
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
}

func TestServerStartupRestoresPendingPublicationTranslationReceipt(t *testing.T) {
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
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "startup-pending", Title: "源标题", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	published, err := contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/initialized", []byte("ready\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	startupBuild := &startupReceiptPublisher{repository: repository, seen: make(chan int, 1)}

	options := Options{
		Repository: repository,
		Version:    "test",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Publisher:  startupBuild,
	}
	app, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := app.translator.List()
	if err != nil || len(tasks) != 1 {
		_ = app.Close()
		t.Fatalf("startup publication receipts = %#v, %v", tasks, err)
	}
	if tasks[0].Status != "failed" || tasks[0].PublicationGeneration != published.Meta.PublicationGeneration || tasks[0].PublicationRevision < 1 || tasks[0].Error != "provider-unavailable" {
		_ = app.Close()
		t.Fatalf("startup receipt = %#v, release=%#v", tasks[0], published.Meta)
	}
	select {
	case seen := <-startupBuild.seen:
		if seen != 1 {
			_ = app.Close()
			t.Fatalf("startup static build observed %d receipts, want 1", seen)
		}
	case <-time.After(2 * time.Second):
		_ = app.Close()
		t.Fatal("startup static build did not run")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	// The durable preflight receipt is generation-matched and is reused on a
	// second startup rather than creating another task for the same publication.
	options.Publisher = fakeSitePublisher{}
	app, err = New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	tasks, err = app.translator.List()
	if err != nil || len(tasks) != 1 || tasks[0].PublicationGeneration != published.Meta.PublicationGeneration {
		t.Fatalf("idempotent startup receipts = %#v, %v", tasks, err)
	}
}
