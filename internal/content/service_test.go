package content

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func testService(t *testing.T) (*Service, *fsrepo.Repository) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{
		SchemaVersion: 1,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
		},
		Fallback: []string{"en", "zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	return NewService(repository), repository
}

func TestPostLifecycleAndManualTranslationProtection(t *testing.T) {
	service, repository := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "hello-world", Title: "你好", SEOTitle: "搜索标题", SEODescription: "搜索描述", Markdown: "# 第一版"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Meta.SourceLocale != "zh-CN" || post.Meta.Revision != 1 || post.Meta.BaseRevision != 1 || post.Meta.HeadRevision != 1 || post.Meta.ReleaseRevision != 0 {
		t.Fatalf("unexpected initial meta: %#v", post.Meta)
	}
	data, err := repository.ReadFile("content/posts/hello-world/zh-CN.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "---\ntitle: 你好\nseoTitle: 搜索标题\nseoDescription: 搜索描述\n---\n\n# 第一版" {
		t.Fatalf("unexpected markdown file:\n%s", data)
	}

	post, err = service.UpdateLocale(post.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: 1, Title: "Hello", Markdown: "# Manual English"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Meta.Locales["en"].Origin != domain.LocaleOriginManual {
		t.Fatalf("origin = %s", post.Meta.Locales["en"].Origin)
	}

	post, err = service.UpdateLocale(post.Meta.ID, "zh-CN", UpdateLocaleInput{ExpectedRevision: 2, Title: "你好", Markdown: "# 第二版"})
	if err != nil {
		t.Fatal(err)
	}
	english := post.Meta.Locales["en"]
	if english.State != "stale" || english.Origin != domain.LocaleOriginManual {
		t.Fatalf("manual translation was not preserved: %#v", english)
	}
	if post.Content["en"].Markdown != "# Manual English" {
		t.Fatal("manual translation content was overwritten")
	}

	post, err = service.PublishPost(post.Meta.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if post.Meta.Status != domain.ContentStatusPublished || post.Meta.PublishedAt == nil {
		t.Fatalf("unexpected published meta: %#v", post.Meta)
	}
	revisions, err := repository.ReadDir("revisions/posts/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 3 {
		t.Fatalf("revision snapshots = %d, want 3", len(revisions))
	}
}

func TestPostIdentityCannotCrossFilePaths(t *testing.T) {
	service, repository := testService(t)
	first, err := service.CreatePost(CreatePostInput{ID: "first-post", Title: "First", Markdown: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreatePost(CreatePostInput{ID: "second-post", Title: "Second", Markdown: "second"})
	if err != nil {
		t.Fatal(err)
	}
	var forged domain.PostMeta
	if err := repository.ReadYAML(postPath(first.Meta.ID, "meta.yaml"), &forged); err != nil {
		t.Fatal(err)
	}
	forged.ID = second.Meta.ID
	if err := repository.WriteYAML(postPath(first.Meta.ID, "meta.yaml"), forged, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateLocale(first.Meta.ID, first.Meta.SourceLocale, UpdateLocaleInput{ExpectedRevision: first.Meta.Revision, Title: "Blocked"}); err == nil {
		t.Fatal("forged post identity was accepted")
	}
	if exists, err := repository.Exists(filepath.Join("revisions", "posts", second.Meta.ID)); err != nil || exists {
		t.Fatalf("forged post created a cross-path revision: exists=%v err=%v", exists, err)
	}
	unchanged, err := service.GetPost(second.Meta.ID)
	if err != nil || unchanged.Content[second.Meta.SourceLocale].Title != "Second" {
		t.Fatalf("second post changed: %#v err=%v", unchanged, err)
	}
}

func TestPostRejectsInvalidIDAndStaleWrite(t *testing.T) {
	service, repository := testService(t)
	if _, err := service.CreatePost(CreatePostInput{ID: "Bad_ID", Title: "No"}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid ID error = %v", err)
	}
	post, err := service.CreatePost(CreatePostInput{Title: "Generated"})
	if err != nil {
		t.Fatal(err)
	}
	if !compactUUIDPattern.MatchString(post.Meta.ID) {
		t.Fatalf("generated ID = %q", post.Meta.ID)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: 1, IDStrategy: IDStrategyTimestamp}, false); err != nil {
		t.Fatal(err)
	}
	timestampPost, err := service.CreatePost(CreatePostInput{Title: "Timestamp post"})
	if err != nil {
		t.Fatal(err)
	}
	timestampPage, err := service.CreatePage(CreatePageInput{Title: "Timestamp page"})
	if err != nil {
		t.Fatal(err)
	}
	if !timestampIDPattern.MatchString(timestampPost.Meta.ID) || !timestampIDPattern.MatchString(timestampPage.Meta.ID) || timestampPost.Meta.ID == timestampPage.Meta.ID {
		t.Fatalf("timestamp IDs = %q, %q", timestampPost.Meta.ID, timestampPage.Meta.ID)
	}
	if _, err := service.CreatePost(CreatePostInput{ID: "1234567890123", Title: "Manual numeric ID"}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("manual timestamp-like ID error = %v", err)
	}
	if _, err := service.CreatePost(CreatePostInput{ID: "duplicate-test", Title: "First"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePost(CreatePostInput{ID: "duplicate-test", Title: "Duplicate"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate ID error = %v", err)
	}
	if _, err := service.UpdateLocale(post.Meta.ID, "zh-CN", UpdateLocaleInput{ExpectedRevision: 99, Title: "Stale"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	if _, err := service.UpdateLocale(post.Meta.ID, "ja", UpdateLocaleInput{ExpectedRevision: 1, Title: "Disabled"}); !errors.Is(err, ErrLocaleDisabled) {
		t.Fatalf("disabled locale error = %v", err)
	}
}

func TestAITranslationDoesNotOverwriteTargetChangedAfterConfirmation(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "target-race", Title: "源文", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.UpdateLocale(post.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "Manual before confirmation", Markdown: "First"})
	if err != nil {
		t.Fatal(err)
	}
	confirmedRevision := post.Meta.Locales["en"].Revision
	post, err = service.UpdateLocale(post.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "Manual after confirmation", Markdown: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{
		ExpectedSourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		ExpectedTargetRevision: &confirmedRevision,
		OverwriteManual:        true,
		Content:                domain.LocalizedMarkdown{Title: "Stale AI result", Markdown: "AI"},
	})
	if !errors.Is(err, ErrTargetChanged) {
		t.Fatalf("ApplyAITranslation() error = %v", err)
	}
	post, err = service.GetPost(post.Meta.ID)
	if err != nil || post.Content["en"].Title != "Manual after confirmation" || post.Meta.Locales["en"].Origin != domain.LocaleOriginManual {
		t.Fatalf("newer manual target was not preserved: %#v, %v", post, err)
	}
}

func TestAITranslationChecksSourceRevisionAndProtectsManualContent(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "translation-test", Title: "源文", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 1, Content: domain.LocalizedMarkdown{Title: "Source", Markdown: "Body"}})
	if err != nil || post.Meta.Locales["en"].Origin != domain.LocaleOriginAI {
		t.Fatalf("AI translation = %#v, err = %v", post, err)
	}
	post, err = service.UpdateLocale(post.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "Manual", Markdown: "Manual body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 1, Content: domain.LocalizedMarkdown{Title: "Overwrite"}}); !errors.Is(err, ErrManualProtected) {
		t.Fatalf("manual protection error = %v", err)
	}
	post, err = service.UpdateLocale(post.Meta.ID, "zh-CN", UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "源文第二版", Markdown: "正文第二版"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 1, OverwriteManual: true, Content: domain.LocalizedMarkdown{Title: "Stale"}}); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("source revision error = %v", err)
	}
	post, err = service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 2, OverwriteManual: true, Content: domain.LocalizedMarkdown{Title: "Fresh", Markdown: "Fresh body"}})
	if err != nil || post.Meta.Locales["en"].Origin != domain.LocaleOriginAI || post.Content["en"].Title != "Fresh" {
		t.Fatalf("forced AI translation = %#v, err = %v", post, err)
	}
}

func TestPageLifecycleUsesIndependentFileTruth(t *testing.T) {
	service, repository := testService(t)
	page, err := service.CreatePage(CreatePageInput{ID: "about-page", Title: "关于", SEOTitle: "关于我们", SEODescription: "站点介绍", Markdown: "# 关于本站"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Kind != "Page" || page.Meta.Template != "page" {
		t.Fatalf("page meta = %#v", page.Meta)
	}
	if page.Content["zh-CN"].SEOTitle != "关于我们" || page.Content["zh-CN"].SEODescription != "站点介绍" {
		t.Fatalf("page SEO = %#v", page.Content["zh-CN"])
	}
	page, err = service.ApplyAIPageTranslation(page.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 1, Content: domain.LocalizedMarkdown{Title: "About", Markdown: "# About this site"}})
	if err != nil || page.Meta.Locales["en"].Origin != domain.LocaleOriginAI {
		t.Fatalf("translated page = %#v, err = %v", page, err)
	}
	page, err = service.PublishPage(page.Meta.ID, page.Meta.Revision)
	if err != nil || page.Meta.Status != domain.ContentStatusPublished {
		t.Fatalf("published page = %#v, err = %v", page, err)
	}
	if _, err := repository.ReadFile("content/pages/about-page/en.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReadDir("revisions/pages/about-page"); err != nil {
		t.Fatal(err)
	}
}

func TestPostSettingsValidateTaxonomyAndLocalMedia(t *testing.T) {
	service, repository := testService(t)
	category := domain.Taxonomy{SchemaVersion: 1, Kind: "Category", ID: "engineering", SourceLocale: "zh-CN", Locales: map[string]domain.LocalizedTaxonomy{"zh-CN": {Name: "工程"}}}
	if err := repository.WriteYAML("content/taxonomies/categories/engineering.yaml", category, false); err != nil {
		t.Fatal(err)
	}
	post, err := service.CreatePost(CreatePostInput{ID: "settings-post", Title: "设置"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.UpdatePostSettings(post.Meta.ID, UpdatePostSettingsInput{ExpectedRevision: post.Meta.Revision, Categories: []string{"engineering", "engineering"}, Cover: "/media/2026/08/cover.webp", CommentPolicy: "closed", Template: "post"})
	if err != nil || len(post.Meta.Categories) != 1 || post.Meta.CommentPolicy != "closed" {
		t.Fatalf("post settings = %#v, err = %v", post.Meta, err)
	}
	if _, err := service.UpdatePostSettings(post.Meta.ID, UpdatePostSettingsInput{ExpectedRevision: post.Meta.Revision, Cover: "https://example.com/cover.jpg"}); err == nil {
		t.Fatal("external cover URL was accepted")
	}
}

func TestPublishedReleaseIsolatedFromHeadEdits(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "release-isolation", Title: "Public title", Markdown: "Public body"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if post.Meta.BaseRevision != 1 || post.Meta.HeadRevision != post.Meta.Revision || post.Meta.ReleaseRevision != post.Meta.Revision || post.Meta.HasUnpublishedChanges {
		t.Fatalf("published pointers = %#v", post.Meta)
	}
	post, err = service.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "Unpublished head", Markdown: "Draft body"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Meta.BaseRevision != 1 || post.Meta.HeadRevision != post.Meta.Revision || post.Meta.ReleaseRevision >= post.Meta.HeadRevision || !post.Meta.HasUnpublishedChanges {
		t.Fatalf("edited pointers = %#v", post.Meta)
	}
	reloaded, err := service.GetPost(post.Meta.ID)
	if err != nil || !reloaded.Meta.HasUnpublishedChanges {
		t.Fatalf("reloaded publication state = %#v, %v", reloaded.Meta, err)
	}
	buildPosts, err := service.ListPostsForBuild()
	if err != nil {
		t.Fatal(err)
	}
	if got := buildPosts[0].Content[post.Meta.SourceLocale].Title; got != "Public title" {
		t.Fatalf("public release leaked head title %q", got)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if post.Meta.HasUnpublishedChanges {
		t.Fatal("explicit publish did not clear unpublished changes")
	}
	buildPosts, err = service.ListPostsForBuild()
	if err != nil {
		t.Fatal(err)
	}
	if got := buildPosts[0].Content[post.Meta.SourceLocale].Title; got != "Unpublished head" {
		t.Fatalf("explicit publish kept old title %q", got)
	}
}

func TestAITranslationPromotesOnlyVerifiedLocale(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "release-translation", Title: "源文", Markdown: "源正文"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 1, Content: domain.LocalizedMarkdown{Title: "Translated", Markdown: "Body"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PromoteAITranslation("Post", post.Meta.ID, "en", 1); err != nil {
		t.Fatal(err)
	}
	post, err = service.GetPost(post.Meta.ID)
	if err != nil || post.Meta.HasUnpublishedChanges {
		t.Fatalf("fully promoted AI translation state = %#v, %v", post.Meta, err)
	}
	buildPosts, err := service.ListPostsForBuild()
	if err != nil {
		t.Fatal(err)
	}
	released := buildPosts[0]
	if released.Content["en"].Title != "Translated" || released.Content["zh-CN"].Title != "源文" {
		t.Fatalf("unexpected promoted release: %#v", released.Content)
	}
}

func TestAITranslationFromUnpublishedSourceStaysOnHead(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "draft-source-translation", Title: "已发布源文", Markdown: "第一版"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "未发布源文", Markdown: "第二版"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.ApplyAITranslation(post.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: 2, Content: domain.LocalizedMarkdown{Title: "Unpublished translation", Markdown: "Second"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PromoteAITranslation("Post", post.Meta.ID, "en", 2); err != nil {
		t.Fatal(err)
	}
	post, err = service.GetPost(post.Meta.ID)
	if err != nil || !post.Meta.HasUnpublishedChanges {
		t.Fatalf("unpublished source edit state = %#v, %v", post.Meta, err)
	}
	buildPosts, err := service.ListPostsForBuild()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := buildPosts[0].Content["en"]; exists || buildPosts[0].Content["zh-CN"].Title != "已发布源文" {
		t.Fatalf("unpublished source translation leaked into release: %#v", buildPosts[0].Content)
	}
}

func TestInitializePublishedReleasesMigratesLegacyHead(t *testing.T) {
	service, repository := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "legacy-release", Title: "Legacy public", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repository.Root(), "releases", "posts", post.Meta.ID)); err != nil {
		t.Fatal(err)
	}
	var legacy domain.PostMeta
	if err := repository.ReadYAML(postPath(post.Meta.ID, "meta.yaml"), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.BaseRevision = 0
	legacy.HeadRevision = 0
	legacy.ReleaseRevision = 0
	if err := repository.WriteYAML(postPath(post.Meta.ID, "meta.yaml"), legacy, false); err != nil {
		t.Fatal(err)
	}
	count, err := service.InitializePublishedReleases()
	if err != nil || count != 1 {
		t.Fatalf("InitializePublishedReleases() = %d, %v", count, err)
	}
	released, err := service.ListPostsForBuild()
	if err != nil || released[0].Content["zh-CN"].Title != "Legacy public" {
		t.Fatalf("migrated release = %#v, %v", released, err)
	}
	if err := repository.ReadYAML(postPath(post.Meta.ID, "meta.yaml"), &legacy); err != nil || legacy.BaseRevision != 1 || legacy.HeadRevision != legacy.Revision || legacy.ReleaseRevision != legacy.Revision {
		t.Fatalf("migrated pointers = %#v, %v", legacy, err)
	}
}

func TestRestoreRevisionCreatesHeadWithoutChangingRelease(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "restore-head", Title: "First", Markdown: "One"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, UpdateLocaleInput{ExpectedRevision: post.Meta.Revision, Title: "Second", Markdown: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := service.ListRevisions("Post", post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	var firstRevision string
	for _, revision := range revisions {
		if revision.Title == "First" {
			firstRevision = revision.ID
			break
		}
	}
	if firstRevision == "" {
		t.Fatal("first revision was not preserved")
	}
	firstSnapshot, err := service.GetRevision("Post", post.Meta.ID, firstRevision)
	if err != nil || firstSnapshot.Content[post.Meta.SourceLocale].Title != "First" {
		t.Fatalf("revision detail = %#v, %v", firstSnapshot, err)
	}
	restored, err := service.RestoreRevision("Post", post.Meta.ID, firstRevision, post.Meta.Revision)
	if err != nil || restored.Content[post.Meta.SourceLocale].Title != "First" {
		t.Fatalf("restored head = %#v, %v", restored, err)
	}
	if !restored.Meta.HasUnpublishedChanges {
		t.Fatal("restored revision was not marked as unpublished")
	}
	if restored.Meta.BaseRevision != 1 || restored.Meta.HeadRevision != restored.Meta.Revision || restored.Meta.ReleaseRevision >= restored.Meta.HeadRevision {
		t.Fatalf("restored pointers = %#v", restored.Meta)
	}
	buildPosts, err := service.ListPostsForBuild()
	if err != nil || buildPosts[0].Content[post.Meta.SourceLocale].Title != "Second" {
		t.Fatalf("release changed during restore: %#v, %v", buildPosts, err)
	}
}
