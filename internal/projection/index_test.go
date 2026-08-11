package projection

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestRebuildIfChangedClaimsEachSourceChangeOnce(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Timezone: "Asia/Shanghai", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}}, CreatedAt: now, UpdatedAt: now}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"zh-CN"}}, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "projection-post", Title: "初始标题", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := Open(repository, contentService, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	if changed, err := service.RebuildIfChanged(); err != nil || changed {
		t.Fatalf("unchanged projection = %v, %v", changed, err)
	}
	if _, err := contentService.UpdateLocale(post.Meta.ID, "zh-CN", content.UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "刷新后的标题", Markdown: "新正文"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := service.RebuildIfChanged(); err != nil || !changed {
		t.Fatalf("changed projection = %v, %v", changed, err)
	}
	if changed, err := service.RebuildIfChanged(); err != nil || changed {
		t.Fatalf("already claimed projection = %v, %v", changed, err)
	}
	results, err := service.Search("刷新后", 10)
	if err != nil || len(results) != 1 || results[0].Title != "刷新后的标题" {
		t.Fatalf("search results = %#v, %v", results, err)
	}
}

func TestManagedProjectionHashSuppressesOnlyItsExactInternalChange(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Timezone: "Asia/Shanghai", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}}, CreatedAt: now, UpdatedAt: now}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}}, Fallback: []string{"zh-CN"}}, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "managed-projection-post", Title: "初始标题", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := Open(repository, contentService, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	if _, err := contentService.UpdateLocale(post.Meta.ID, "zh-CN", content.UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "内部更新", Markdown: "正文"}); err != nil {
		t.Fatal(err)
	}
	managedHash, err := service.fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.managedHash = managedHash
	service.mu.Unlock()
	if changed, err := service.RebuildIfChanged(); err != nil || changed {
		t.Fatalf("exact managed hash = changed %v, err %v", changed, err)
	}

	currentHash, err := service.fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.managedHash = "older-managed-hash"
	service.lastHash = currentHash
	service.stats.Status = "ready"
	service.mu.Unlock()
	if changed, err := service.RebuildIfChanged(); err != nil || !changed {
		t.Fatalf("indexed external divergence = changed %v, err %v", changed, err)
	}
}
