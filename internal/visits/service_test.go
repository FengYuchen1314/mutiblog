package visits

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
)

func TestVisitsPersistOnlyAggregateAndBuildPublicLocalizedSnapshot(t *testing.T) {
	service, repository, firstID, secondID, pageID := testVisitService(t)
	if result, err := service.Record("Post", firstID, "203.0.113.10"); err != nil || !result.Counted || result.Count != 1 {
		t.Fatalf("first visit = %#v, %v", result, err)
	}
	if _, err := service.Record("Post", secondID, "203.0.113.11"); err != nil {
		t.Fatal(err)
	}
	if result, err := service.Record("posts", secondID, "203.0.113.12"); err != nil || result.Count != 2 {
		t.Fatalf("second post visit = %#v, %v", result, err)
	}
	if _, err := service.Record("Page", pageID, "203.0.113.13"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Record("Post", firstID, "203.0.113.10"); !errors.Is(err, ErrRateLimit) {
		t.Fatalf("duplicate address visit error = %v", err)
	}

	data, err := repository.ReadFile(counterPath("Post", firstID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "203.0.113.10") || strings.Contains(string(data), "hmac") {
		t.Fatalf("persistent visit counter leaked visitor-derived data: %s", data)
	}

	reopened := NewService(repository, content.NewService(repository))
	snapshot, err := reopened.PublicSnapshot("en", 5)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Profile.Posts != 2 || snapshot.Profile.Categories != 1 || snapshot.Profile.Visits != 4 {
		t.Fatalf("profile = %#v", snapshot.Profile)
	}
	if len(snapshot.PopularPosts) != 2 || snapshot.PopularPosts[0].ID != secondID || snapshot.PopularPosts[0].Visits != 2 || snapshot.PopularPosts[0].Title != "Second post" {
		t.Fatalf("popular posts = %#v", snapshot.PopularPosts)
	}
	for _, key := range []string{SubjectKey("Post", firstID), SubjectKey("Post", secondID), SubjectKey("Page", pageID)} {
		if _, ok := snapshot.PublicSubjects[key]; !ok {
			t.Fatalf("public subject %q missing", key)
		}
	}
	if err := reopened.ValidateAll(); err != nil {
		t.Fatalf("ValidateAll() = %v", err)
	}
}

func TestVisitsRejectDraftPrivateInvalidAndIdentityMismatch(t *testing.T) {
	service, repository, firstID, _, _ := testVisitService(t)
	contentService := content.NewService(repository)
	draft, err := contentService.CreatePost(content.CreatePostInput{ID: "draft-visit", Title: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Record("Post", draft.Meta.ID, "203.0.113.20"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft visit error = %v", err)
	}
	private, err := contentService.CreatePost(content.CreatePostInput{ID: "private-visit", Title: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	visibility := domain.ContentVisibilityPrivate
	private, err = contentService.UpdatePostSettings(private.Meta.ID, content.UpdatePostSettingsInput{ExpectedRevision: private.Meta.Revision, Visibility: &visibility, CommentPolicy: "open", Template: "post"})
	if err != nil {
		t.Fatal(err)
	}
	private, err = contentService.PublishPost(private.Meta.ID, private.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Record("Post", private.Meta.ID, "203.0.113.21"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private visit error = %v", err)
	}
	if _, err := service.Record("Unknown", firstID, "203.0.113.22"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid kind error = %v", err)
	}
	if _, err := service.Record("Post", firstID, "not-an-ip"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid IP error = %v", err)
	}
	if _, err := service.PublicSnapshot("not a locale", 5); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid locale error = %v", err)
	}

	if _, err := service.Record("Post", firstID, "203.0.113.23"); err != nil {
		t.Fatal(err)
	}
	var counter storedCounter
	if err := repository.ReadYAML(counterPath("Post", firstID), &counter); err != nil {
		t.Fatal(err)
	}
	counter.SubjectIdentity = strings.Repeat("f", 64)
	if err := repository.WriteYAML(counterPath("Post", firstID), counter, false); err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateAll(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("identity mismatch ValidateAll() = %v", err)
	}
	overflow := newCounter("Post", firstID, strings.Repeat("a", 64))
	overflow.Count = maxStoredVisits + 1
	overflow.UpdatedAt = time.Now().UTC()
	if validCounter(overflow, "Post", firstID) {
		t.Fatal("counter beyond the exact public JSON range was accepted")
	}
}

func TestSelectLocalizedUsesChineseBeforeEntitySource(t *testing.T) {
	subject := domain.Post{
		Meta: domain.PostMeta{SourceLocale: "en"},
		Content: map[string]domain.LocalizedMarkdown{
			"en":    {Title: "English source"},
			"zh-CN": {Title: "中文回退"},
		},
	}
	config := domain.LocalesConfig{Enabled: []domain.LocaleDefinition{
		{Code: "ja", Enabled: true},
		{Code: "en", Enabled: true},
		{Code: "zh-CN", Enabled: true},
	}}
	localized, ok := selectLocalized(subject, "ja", config)
	if !ok || localized.Title != "中文回退" {
		t.Fatalf("localized fallback = %#v, %v", localized, ok)
	}
}

func TestFailedLocaleIsNotPubliclyEnabled(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{Enabled: []domain.LocaleDefinition{
		{Code: "zh-CN", Enabled: true, Status: domain.LocaleStatusReady},
		{Code: "ja", Enabled: true, Status: domain.LocaleStatusFailed},
	}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, content.NewService(repository))
	if _, _, err := service.enabledLocale("ja"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed locale enabledLocale() error = %v, want ErrInvalid", err)
	}
	if localized, ok := selectLocalized(domain.Post{
		Meta:    domain.PostMeta{SourceLocale: "zh-CN"},
		Content: map[string]domain.LocalizedMarkdown{"zh-CN": {Title: "不得回退"}},
	}, "ja", domain.LocalesConfig{Enabled: []domain.LocaleDefinition{
		{Code: "zh-CN", Enabled: true, Status: domain.LocaleStatusReady},
		{Code: "ja", Enabled: true, Status: domain.LocaleStatusFailed},
	}}); ok || localized.Title != "" {
		t.Fatalf("failed locale fallback leaked = %#v, %v", localized, ok)
	}
}

func TestVisitLimiterHasStrictBoundExpiresAndFailsClosed(t *testing.T) {
	now := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	service := &Service{recent: make(map[string]time.Time)}
	for index := 0; index < maxVisitKeys; index++ {
		if !service.allowLocked(fmt.Sprintf("visitor-%d", index), now) {
			t.Fatalf("visitor %d rejected before hard limit", index)
		}
	}
	if service.allowLocked("overflow", now) {
		t.Fatal("new visitor was accepted beyond hard limit")
	}
	if len(service.recent) != maxVisitKeys {
		t.Fatalf("limiter keys = %d, want %d", len(service.recent), maxVisitKeys)
	}
	if !service.allowLocked("replacement", now.Add(visitWindow+time.Second)) {
		t.Fatal("expired limiter entries were not reclaimed")
	}
	if len(service.recent) > maxVisitKeys {
		t.Fatalf("limiter grew beyond hard limit: %d", len(service.recent))
	}
}

func TestVisitWriteFailureDoesNotConsumeDeduplicationWindow(t *testing.T) {
	service, repository, firstID, _, _ := testVisitService(t)
	directory := filepath.Join(repository.Root(), "visits", "posts")
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory, []byte("blocks visit directory"), 0o640); err != nil {
		t.Fatal(err)
	}
	if result, err := service.Record("Post", firstID, "203.0.113.50"); err == nil || result.Counted {
		t.Fatalf("failed visit write = %#v, %v", result, err)
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if result, err := service.Record("Post", firstID, "203.0.113.50"); err != nil || !result.Counted || result.Count != 1 {
		t.Fatalf("retry visit = %#v, %v", result, err)
	}
}

func TestPermanentContentDeletionRemovesVisitCounter(t *testing.T) {
	service, repository, firstID, _, _ := testVisitService(t)
	if _, err := service.Record("Post", firstID, "203.0.113.30"); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	head, err := contentService.GetPost(firstID)
	if err != nil {
		t.Fatal(err)
	}
	recycled, err := contentService.ChangeStatus("Post", firstID, "recycle", head.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentService.DeleteRecycled("Post", firstID, recycled.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	if exists, err := repository.Exists(counterPath("Post", firstID)); err != nil || exists {
		t.Fatalf("deleted visit counter exists = %v, %v", exists, err)
	}
}

func testVisitService(t *testing.T) (*Service, *fsrepo.Repository, string, string, string) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled: []domain.LocaleDefinition{
			{Code: "en", Label: "English", Enabled: true},
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}, CommentHMACKey: strings.Repeat("1", 64)}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := taxonomy.NewService(repository).Create("Category", taxonomy.CreateInput{ID: "engineering", Name: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	createPost := func(id, title string, categories []string) domain.Post {
		post, err := contentService.CreatePost(content.CreatePostInput{ID: id, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		post, err = contentService.UpdatePostSettings(post.Meta.ID, content.UpdatePostSettingsInput{ExpectedRevision: post.Meta.Revision, Categories: categories, CommentPolicy: "open", Template: "post"})
		if err != nil {
			t.Fatal(err)
		}
		post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
		if err != nil {
			t.Fatal(err)
		}
		post, err = contentService.ApplyAITranslation(post.Meta.ID, "zh-CN", content.ApplyAITranslationInput{
			ExpectedSourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
			Content:                domain.LocalizedMarkdown{Title: "本地化文章"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := contentService.PromoteAITranslation("Post", post.Meta.ID, "zh-CN", post.Meta.Locales[post.Meta.SourceLocale].Revision); err != nil {
			t.Fatal(err)
		}
		return post
	}
	first := createPost("first-visit-post", "First post", []string{"engineering"})
	second := createPost("second-visit-post", "Second post", nil)
	page, err := contentService.CreatePage(content.CreatePageInput{ID: "visited-page", Title: "Visited page"})
	if err != nil {
		t.Fatal(err)
	}
	page, err = contentService.PublishPage(page.Meta.ID, page.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	page, err = contentService.ApplyAIPageTranslation(page.Meta.ID, "zh-CN", content.ApplyAITranslationInput{
		ExpectedSourceRevision: page.Meta.Locales[page.Meta.SourceLocale].Revision,
		Content:                domain.LocalizedMarkdown{Title: "本地化页面"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := contentService.PromoteAITranslation("Page", page.Meta.ID, "zh-CN", page.Meta.Locales[page.Meta.SourceLocale].Revision); err != nil {
		t.Fatal(err)
	}
	return NewService(repository, contentService), repository, first.Meta.ID, second.Meta.ID, page.Meta.ID
}
