package content

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
		Fallback: []string{"zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	return NewService(repository), repository
}

type contentReadResult struct {
	item domain.Post
	err  error
}

type contentListResult struct {
	items []domain.Post
	err   error
}

func awaitContentRead(t *testing.T, result <-chan contentReadResult) contentReadResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("content read remained blocked after the commit completed")
		return contentReadResult{}
	}
}

func awaitContentList(t *testing.T, result <-chan contentListResult) contentListResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("content list remained blocked after the commit completed")
		return contentListResult{}
	}
}

func TestPublicReadsWaitForCompleteContentCommit(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			service, repository := testService(t)
			var item domain.Post
			var err error
			if kind == "Post" {
				item, err = service.CreatePost(CreatePostInput{ID: "atomic-read", Title: "源文", Markdown: "正文"})
				if err == nil {
					item, err = service.ApplyAITranslation(item.Meta.ID, "en", ApplyAITranslationInput{
						ExpectedSourceRevision: 1,
						Content:                domain.LocalizedMarkdown{Title: "English v1", Markdown: "Body v1"},
					})
				}
			} else {
				item, err = service.CreatePage(CreatePageInput{ID: "atomic-read", Title: "源文", Markdown: "正文"})
				if err == nil {
					item, err = service.ApplyAIPageTranslation(item.Meta.ID, "en", ApplyAITranslationInput{
						ExpectedSourceRevision: 1,
						Content:                domain.LocalizedMarkdown{Title: "English v1", Markdown: "Body v1"},
					})
				}
			}
			if err != nil {
				t.Fatal(err)
			}

			nextContent := domain.LocalizedMarkdown{Title: "English v2", Markdown: "Body v2"}
			nextMeta := item.Meta
			nextMeta.Locales = make(map[string]domain.LocaleContentState, len(item.Meta.Locales))
			for locale, state := range item.Meta.Locales {
				nextMeta.Locales[locale] = state
			}
			nextState := nextMeta.Locales["en"]
			nextState.Revision++
			nextMeta.Locales["en"] = nextState
			advanceHead(&nextMeta)
			nextMeta.UpdatedAt = item.Meta.UpdatedAt.Add(time.Second)

			var get func() (domain.Post, error)
			var list func() ([]domain.Post, error)
			var writeLocale func() error
			metaPath := postPath(item.Meta.ID, "meta.yaml")
			if kind == "Post" {
				get = func() (domain.Post, error) { return service.GetPost(item.Meta.ID) }
				list = service.ListPosts
				writeLocale = func() error { return service.writeLocale(item.Meta.ID, "en", nextContent) }
			} else {
				get = func() (domain.Post, error) { return service.GetPage(item.Meta.ID) }
				list = service.ListPages
				writeLocale = func() error { return service.writePageLocale(item.Meta.ID, "en", nextContent) }
				metaPath = pagePath(item.Meta.ID, "meta.yaml")
			}

			service.mu.Lock()
			if err := writeLocale(); err != nil {
				service.mu.Unlock()
				t.Fatal(err)
			}

			getStarted := make(chan struct{})
			getDone := make(chan contentReadResult, 1)
			go func() {
				getStarted <- struct{}{}
				got, err := get()
				getDone <- contentReadResult{item: got, err: err}
			}()
			<-getStarted
			runtime.Gosched()

			listStarted := make(chan struct{})
			listDone := make(chan contentListResult, 1)
			go func() {
				listStarted <- struct{}{}
				got, err := list()
				listDone <- contentListResult{items: got, err: err}
			}()
			<-listStarted
			runtime.Gosched()

			var getResult contentReadResult
			var listResult contentListResult
			getReturnedEarly := false
			listReturnedEarly := false
			select {
			case getResult = <-getDone:
				getReturnedEarly = true
			default:
			}
			select {
			case listResult = <-listDone:
				listReturnedEarly = true
			default:
			}

			if err := repository.WriteYAML(metaPath, nextMeta, false); err != nil {
				service.mu.Unlock()
				t.Fatal(err)
			}
			service.mu.Unlock()

			if !getReturnedEarly {
				getResult = awaitContentRead(t, getDone)
			}
			if !listReturnedEarly {
				listResult = awaitContentList(t, listDone)
			}
			if getReturnedEarly || listReturnedEarly {
				t.Fatalf("read crossed an in-progress %s commit: Get returned early=%t, List returned early=%t", kind, getReturnedEarly, listReturnedEarly)
			}
			if getResult.err != nil {
				t.Fatalf("Get%s() error = %v", kind, getResult.err)
			}
			if listResult.err != nil {
				t.Fatalf("List%ss() error = %v", kind, listResult.err)
			}
			if getResult.item.Meta.Revision != nextMeta.Revision || getResult.item.Meta.Locales["en"].Revision != nextState.Revision || getResult.item.Content["en"] != nextContent {
				t.Fatalf("Get%s() returned a torn revision: %#v", kind, getResult.item)
			}
			if len(listResult.items) != 1 || listResult.items[0].Meta.Revision != nextMeta.Revision || listResult.items[0].Meta.Locales["en"].Revision != nextState.Revision || listResult.items[0].Content["en"] != nextContent {
				t.Fatalf("List%ss() returned a torn revision: %#v", kind, listResult.items)
			}
		})
	}
}

func TestLockedListsDoNotReenterReadLockWithWaitingWriter(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	service, _ := testService(t)
	if _, err := service.CreatePost(CreatePostInput{ID: "post-lock", Title: "Post"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePage(CreatePageInput{ID: "page-lock", Title: "Page"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		list func() ([]domain.Post, error)
	}{
		{name: "Post", list: service.listPostsLocked},
		{name: "Page", list: service.listPagesLocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			service.mu.RLock()
			writerStarted := make(chan struct{})
			writerDone := make(chan struct{})
			go func() {
				writerStarted <- struct{}{}
				service.mu.Lock()
				service.mu.Unlock()
				close(writerDone)
			}()
			<-writerStarted
			runtime.Gosched()

			listDone := make(chan error, 1)
			go func() {
				_, err := test.list()
				listDone <- err
			}()
			var listErr error
			select {
			case err := <-listDone:
				listErr = err
				service.mu.RUnlock()
			case <-time.After(2 * time.Second):
				service.mu.RUnlock()
				<-writerDone
				<-listDone
				t.Fatal("locked list re-entered the read lock while a writer was waiting")
			}
			<-writerDone
			if listErr != nil {
				t.Fatal(listErr)
			}
		})
	}
}

func TestPostAndPageCoreBehaviorParity(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		plural    string
		template  string
		create    func(*Service, CreatePostInput) (domain.Post, error)
		get       func(*Service, string) (domain.Post, error)
		list      func(*Service) ([]domain.Post, error)
		update    func(*Service, string, string, UpdateLocaleInput) (domain.Post, error)
		translate func(*Service, string, string, ApplyAITranslationInput) (domain.Post, error)
		publish   func(*Service, string, int) (domain.Post, error)
	}{
		{
			name: "Post", kind: "Post", plural: "posts", template: "post",
			create: (*Service).CreatePost, get: (*Service).GetPost, list: (*Service).ListPosts,
			update: (*Service).UpdateLocale, translate: (*Service).ApplyAITranslation, publish: (*Service).PublishPost,
		},
		{
			name: "Page", kind: "Page", plural: "pages", template: "page",
			create: (*Service).CreatePage, get: (*Service).GetPage, list: (*Service).ListPages,
			update: (*Service).UpdatePageLocale, translate: (*Service).ApplyAIPageTranslation, publish: (*Service).PublishPage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, repository := testService(t)
			item, err := test.create(service, CreatePostInput{
				ID: "shared-core", Title: "  Source title  ", Summary: "  Source summary  ",
				SEOTitle: "  SEO title  ", SEODescription: "  SEO description  ", Markdown: "Source body",
			})
			if err != nil {
				t.Fatal(err)
			}
			sourceLocale := item.Meta.SourceLocale
			localized := item.Content[sourceLocale]
			if item.Meta.Kind != test.kind || item.Meta.Template != test.template || item.Meta.Revision != 1 || localized.Title != "Source title" || localized.Summary != "Source summary" || localized.SEOTitle != "SEO title" || localized.SEODescription != "SEO description" || localized.Markdown != "Source body" {
				t.Fatalf("created %s = %#v", test.name, item)
			}
			contentPath := filepath.Join("content", test.plural, item.Meta.ID)
			if exists, pathErr := repository.Exists(filepath.Join(contentPath, "meta.yaml")); pathErr != nil || !exists {
				t.Fatalf("%s metadata path exists = %t, err = %v", test.name, exists, pathErr)
			}
			oppositePlural := "pages"
			if test.plural == "pages" {
				oppositePlural = "posts"
			}
			if exists, pathErr := repository.Exists(filepath.Join("content", oppositePlural, item.Meta.ID, "meta.yaml")); pathErr != nil || exists {
				t.Fatalf("%s crossed into %s path: exists = %t, err = %v", test.name, oppositePlural, exists, pathErr)
			}

			if _, err := test.update(service, item.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: 99, Title: "Stale"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale %s update error = %v", test.name, err)
			}
			item, err = test.update(service, item.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: item.Meta.Revision, Title: "  Manual  ", Markdown: "Manual body"})
			if err != nil {
				t.Fatal(err)
			}
			manualState := item.Meta.Locales["en"]
			if item.Meta.Revision != 2 || manualState.Revision != 1 || manualState.Origin != domain.LocaleOriginManual || item.Content["en"].Title != "Manual" {
				t.Fatalf("manual %s locale = %#v", test.name, item)
			}

			wrongTargetRevision := manualState.Revision - 1
			_, err = test.translate(service, item.Meta.ID, "en", ApplyAITranslationInput{
				ExpectedSourceRevision: item.Meta.Locales[sourceLocale].Revision,
				ExpectedTargetRevision: &wrongTargetRevision,
				OverwriteManual:        true,
				Content:                domain.LocalizedMarkdown{Title: "Wrong target"},
			})
			if !errors.Is(err, ErrTargetChanged) {
				t.Fatalf("changed %s target error = %v", test.name, err)
			}
			_, err = test.translate(service, item.Meta.ID, "en", ApplyAITranslationInput{
				ExpectedSourceRevision: item.Meta.Locales[sourceLocale].Revision,
				Content:                domain.LocalizedMarkdown{Title: "Protected"},
			})
			if !errors.Is(err, ErrManualProtected) {
				t.Fatalf("manual %s protection error = %v", test.name, err)
			}

			item, err = test.translate(service, item.Meta.ID, "en", ApplyAITranslationInput{
				ExpectedSourceRevision: item.Meta.Locales[sourceLocale].Revision,
				ExpectedTargetRevision: &manualState.Revision,
				OverwriteManual:        true,
				Content:                domain.LocalizedMarkdown{Title: "  AI title  ", Summary: "  AI summary  ", Markdown: "AI body"},
			})
			if err != nil {
				t.Fatal(err)
			}
			aiState := item.Meta.Locales["en"]
			if item.Meta.Revision != 3 || aiState.Revision != 2 || aiState.Origin != domain.LocaleOriginAI || item.Content["en"].Title != "AI title" || item.Content["en"].Summary != "AI summary" {
				t.Fatalf("AI %s locale = %#v", test.name, item)
			}

			item, err = test.update(service, item.Meta.ID, sourceLocale, UpdateLocaleInput{ExpectedRevision: item.Meta.Revision, Title: "Source v2", Markdown: "Source body v2"})
			if err != nil {
				t.Fatal(err)
			}
			if item.Meta.Revision != 4 || item.Meta.Locales[sourceLocale].Revision != 2 || item.Meta.Locales["en"].State != "stale" || item.Meta.Locales["en"].Origin != domain.LocaleOriginAI {
				t.Fatalf("updated %s source = %#v", test.name, item)
			}
			if _, err := test.publish(service, item.Meta.ID, item.Meta.Revision-1); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale %s publish error = %v", test.name, err)
			}
			item, err = test.publish(service, item.Meta.ID, item.Meta.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if item.Meta.Status != domain.ContentStatusPublished || item.Meta.Revision != 5 || item.Meta.ReleaseRevision != item.Meta.Revision || item.Meta.HasUnpublishedChanges {
				t.Fatalf("published %s = %#v", test.name, item.Meta)
			}

			got, err := test.get(service, item.Meta.ID)
			if err != nil || got.Meta.Kind != test.kind || got.Meta.Revision != item.Meta.Revision || got.Content["en"].Title != "AI title" {
				t.Fatalf("Get%s() = %#v, err = %v", test.name, got, err)
			}
			listed, err := test.list(service)
			if err != nil || len(listed) != 1 || listed[0].Meta.ID != item.Meta.ID || listed[0].Meta.Kind != test.kind {
				t.Fatalf("List%ss() = %#v, err = %v", test.name, listed, err)
			}
			if exists, releaseErr := repository.Exists(filepath.Join("releases", test.plural, item.Meta.ID, "current.yaml")); releaseErr != nil || !exists {
				t.Fatalf("%s release exists = %t, err = %v", test.name, exists, releaseErr)
			}

			if err := repository.WriteFile(filepath.Join(contentPath, sourceLocale+".md"), []byte("broken markdown"), 0o640); err != nil {
				t.Fatal(err)
			}
			_, err = test.get(service, item.Meta.ID)
			wantDecodeError := "decode " + item.Meta.ID + " locale " + sourceLocale + ": markdown front matter is missing"
			if test.kind == "Page" {
				wantDecodeError = "decode page " + item.Meta.ID + " locale " + sourceLocale + ": markdown front matter is missing"
			}
			if err == nil || err.Error() != wantDecodeError {
				t.Fatalf("%s decode error = %v, want %q", test.name, err, wantDecodeError)
			}
		})
	}
}

func TestCommitPublicationPreservesReleaseAndRollbackErrors(t *testing.T) {
	releaseFailure := errors.New("release failed")
	rollbackFailure := errors.New("rollback failed")
	previous := domain.PostMeta{Revision: 4}
	published := domain.PostMeta{Revision: 5}
	writes := make([]int, 0, 2)
	err := commitPublication(previous, published, func(meta domain.PostMeta) error {
		writes = append(writes, meta.Revision)
		if meta.Revision == previous.Revision {
			return rollbackFailure
		}
		return nil
	}, func() error { return releaseFailure })
	if !errors.Is(err, releaseFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("commitPublication() error = %v; want joined release and rollback errors", err)
	}
	if len(writes) != 2 || writes[0] != published.Revision || writes[1] != previous.Revision {
		t.Fatalf("metadata write order = %v, want [%d %d]", writes, published.Revision, previous.Revision)
	}

	writes = writes[:0]
	err = commitPublication(previous, published, func(meta domain.PostMeta) error {
		writes = append(writes, meta.Revision)
		return nil
	}, func() error { return releaseFailure })
	if !errors.Is(err, releaseFailure) || errors.Is(err, rollbackFailure) {
		t.Fatalf("commitPublication() rollback-success error = %v", err)
	}
	if len(writes) != 2 || writes[1] != previous.Revision {
		t.Fatalf("successful rollback writes = %v", writes)
	}

	metadataFailure := errors.New("metadata failed")
	releaseCalled := false
	err = commitPublication(previous, published, func(domain.PostMeta) error { return metadataFailure }, func() error {
		releaseCalled = true
		return nil
	})
	if !errors.Is(err, metadataFailure) || releaseCalled {
		t.Fatalf("initial metadata failure = %v, releaseCalled = %t", err, releaseCalled)
	}
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
	if len(revisions) != 4 {
		t.Fatalf("revision snapshots = %d, want 4", len(revisions))
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
	pinned := true
	visibility := domain.ContentVisibilityPrivate
	publishedAt := time.Date(2026, time.August, 12, 8, 30, 0, 0, time.UTC)
	post, err = service.UpdatePostSettings(post.Meta.ID, UpdatePostSettingsInput{
		ExpectedRevision: post.Meta.Revision,
		Categories:       []string{"engineering", "engineering"},
		Cover:            "/media/2026/08/cover.webp",
		Pinned:           &pinned,
		Visibility:       &visibility,
		PublishedAt:      &publishedAt,
		PublishTimeSet:   true,
		CommentPolicy:    "closed",
		Template:         "post",
	})
	if err != nil || len(post.Meta.Categories) != 1 || post.Meta.CommentPolicy != "closed" || !post.Meta.Pinned || post.Meta.Visibility != domain.ContentVisibilityPrivate || post.Meta.PublishedAt == nil || !post.Meta.PublishedAt.Equal(publishedAt) {
		t.Fatalf("post settings = %#v, err = %v", post.Meta, err)
	}
	if _, err := service.UpdatePostSettings(post.Meta.ID, UpdatePostSettingsInput{ExpectedRevision: post.Meta.Revision, Cover: "https://example.com/cover.jpg"}); err == nil {
		t.Fatal("external cover URL was accepted")
	}
}

func TestPrivateReleaseIsExcludedFromPublicBuildAndComments(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "private-post", Title: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	visibility := domain.ContentVisibilityPrivate
	post, err = service.UpdatePostSettings(post.Meta.ID, UpdatePostSettingsInput{ExpectedRevision: post.Meta.Revision, Visibility: &visibility})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListPostsForBuild()
	if err != nil {
		t.Fatal(err)
	}
	stillPublic := false
	for _, item := range items {
		if item.Meta.ID == post.Meta.ID {
			stillPublic = true
		}
	}
	if !stillPublic {
		t.Fatal("unpublished visibility edit changed the active public release")
	}
	if _, err := service.GetPublishedRelease("Post", post.Meta.ID); err != nil {
		t.Fatalf("public release disappeared before explicit publish: %v", err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	items, err = service.ListPostsForBuild()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Meta.ID == post.Meta.ID {
			t.Fatal("private post leaked into the public build")
		}
	}
	if _, err := service.GetPublishedRelease("Post", post.Meta.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private release lookup error = %v, want ErrNotFound", err)
	}
}

func TestLegacyContentDefaultsToPublicVisibility(t *testing.T) {
	service, repository := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "legacy-visibility", Title: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	var meta domain.PostMeta
	if err := repository.ReadYAML(postPath(post.Meta.ID, "meta.yaml"), &meta); err != nil {
		t.Fatal(err)
	}
	meta.Visibility = ""
	if err := repository.WriteYAML(postPath(post.Meta.ID, "meta.yaml"), meta, false); err != nil {
		t.Fatal(err)
	}
	reloaded, err := service.GetPost(post.Meta.ID)
	if err != nil || reloaded.Meta.Visibility != domain.ContentVisibilityPublic {
		t.Fatalf("legacy visibility = %q, err = %v", reloaded.Meta.Visibility, err)
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

func TestAITranslationReleaseUsesExactHistorySnapshotAndIsIdempotent(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			service, repository := testService(t)
			var item domain.Post
			var err error
			if kind == "Post" {
				item, err = service.CreatePost(CreatePostInput{ID: "release-history", Title: "源文", Markdown: "源正文"})
			} else {
				item, err = service.CreatePage(CreatePageInput{ID: "release-history", Title: "源文", Markdown: "源正文"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "Post" {
				item, err = service.PublishPost(item.Meta.ID, item.Meta.Revision)
			} else {
				item, err = service.PublishPage(item.Meta.ID, item.Meta.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			visibility := item.Meta.Visibility
			pinned := item.Meta.Pinned
			settings := UpdatePostSettingsInput{
				ExpectedRevision: item.Meta.Revision,
				Categories:       item.Meta.Categories,
				Tags:             item.Meta.Tags,
				Cover:            "/media/unpublished-cover.webp",
				Pinned:           &pinned,
				Visibility:       &visibility,
				CommentPolicy:    item.Meta.CommentPolicy,
				Template:         item.Meta.Template,
			}
			if kind == "Post" {
				item, err = service.UpdatePostSettings(item.Meta.ID, settings)
			} else {
				item, err = service.UpdatePageSettings(item.Meta.ID, settings)
			}
			if err != nil {
				t.Fatal(err)
			}
			sourceRevision := item.Meta.Locales[item.Meta.SourceLocale].Revision
			translation := ApplyAITranslationInput{ExpectedSourceRevision: sourceRevision, Content: domain.LocalizedMarkdown{Title: "Translated", Markdown: "Body"}}
			if kind == "Post" {
				item, err = service.ApplyAITranslation(item.Meta.ID, "en", translation)
			} else {
				item, err = service.ApplyAIPageTranslation(item.Meta.ID, "en", translation)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := service.PromoteAITranslation(kind, item.Meta.ID, "en", sourceRevision); err != nil {
				t.Fatal(err)
			}
			root, err := releaseRoot(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			var pointer releasePointer
			if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &pointer); err != nil {
				t.Fatal(err)
			}
			if pointer.HistorySnapshot == "" {
				t.Fatal("release pointer did not record an exact history snapshot")
			}
			revisions, err := service.ListRevisions(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			releaseCount := 0
			foundUnpublishedCollision := false
			for _, revision := range revisions {
				if revision.IsRelease {
					releaseCount++
					if revision.ID != pointer.HistorySnapshot {
						t.Fatalf("release history ID = %q, want pointer history snapshot %q", revision.ID, pointer.HistorySnapshot)
					}
				}
				if revision.Revision == pointer.Revision && revision.ID != pointer.HistorySnapshot {
					snapshot, err := service.GetRevision(kind, item.Meta.ID, revision.ID)
					if err != nil {
						t.Fatal(err)
					}
					if snapshot.Meta.Cover == settings.Cover {
						foundUnpublishedCollision = true
					}
				}
			}
			if releaseCount != 1 || !foundUnpublishedCollision {
				t.Fatalf("releaseCount = %d, foundUnpublishedCollision = %t, revisions = %#v", releaseCount, foundUnpublishedCollision, revisions)
			}
			releaseHistory, err := service.GetRevision(kind, item.Meta.ID, pointer.HistorySnapshot)
			if err != nil {
				t.Fatal(err)
			}
			if releaseHistory.Content["en"].Title != "Translated" || releaseHistory.Meta.Cover != "" {
				t.Fatalf("release history mixed unpublished head state: %#v", releaseHistory)
			}
			paths, err := lifecyclePaths(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeRetry, err := repository.ReadDir(paths.revisions)
			if err != nil {
				t.Fatal(err)
			}
			var rawHead domain.PostMeta
			if err := repository.ReadYAML(filepath.Join(paths.content, "meta.yaml"), &rawHead); err != nil {
				t.Fatal(err)
			}
			rawHead.ReleaseRevision = 0
			if err := repository.WriteYAML(filepath.Join(paths.content, "meta.yaml"), rawHead, false); err != nil {
				t.Fatal(err)
			}
			if err := service.PromoteAITranslation(kind, item.Meta.ID, "en", sourceRevision); err != nil {
				t.Fatal(err)
			}
			var retriedPointer releasePointer
			if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &retriedPointer); err != nil {
				t.Fatal(err)
			}
			afterRetry, err := repository.ReadDir(paths.revisions)
			if err != nil {
				t.Fatal(err)
			}
			if retriedPointer.Snapshot != pointer.Snapshot || retriedPointer.HistorySnapshot != pointer.HistorySnapshot || retriedPointer.Revision != pointer.Revision || len(afterRetry) != len(beforeRetry) {
				t.Fatalf("retry changed release: before=%#v/%d after=%#v/%d", pointer, len(beforeRetry), retriedPointer, len(afterRetry))
			}
			if err := repository.ReadYAML(filepath.Join(paths.content, "meta.yaml"), &rawHead); err != nil {
				t.Fatal(err)
			}
			if rawHead.ReleaseRevision != pointer.Revision {
				t.Fatalf("persisted head release revision = %d, want %d", rawHead.ReleaseRevision, pointer.Revision)
			}
			var head domain.Post
			if kind == "Post" {
				head, err = service.GetPost(item.Meta.ID)
			} else {
				head, err = service.GetPage(item.Meta.ID)
			}
			if err != nil || head.Meta.ReleaseRevision != pointer.Revision || !head.Meta.HasUnpublishedChanges {
				t.Fatalf("head after idempotent promotion = %#v, %v", head.Meta, err)
			}
		})
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

func TestInitializePublishedReleasesMigratesExactHistoryIdentityWithoutOverwritingCollision(t *testing.T) {
	service, repository := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "legacy-release-history", Title: "Public", Markdown: "Release body"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = service.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	root, err := releaseRoot("Post", post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	var legacy releasePointer
	if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &legacy); err != nil {
		t.Fatal(err)
	}
	originalReleaseSnapshot := legacy.Snapshot
	legacy.HistorySnapshot = ""
	legacyUpdatedAt := legacy.UpdatedAt
	if err := repository.WriteYAML(filepath.Join(root, "current.yaml"), legacy, false); err != nil {
		t.Fatal(err)
	}
	paths, err := lifecyclePaths("Post", post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := post
	conflicting.Meta.Cover = "/media/unpublished.webp"
	conflicting.Content[post.Meta.SourceLocale] = domain.LocalizedMarkdown{Title: "Unrelated history", Markdown: "Not the release"}
	if err := service.writeContentSnapshot(filepath.Join(paths.revisions, originalReleaseSnapshot), conflicting); err != nil {
		t.Fatal(err)
	}
	post, err = service.ChangeStatus("Post", post.Meta.ID, "unpublish", post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}

	initialized, err := service.InitializePublishedReleases()
	if err != nil || initialized != 1 {
		t.Fatalf("InitializePublishedReleases() = %d, %v", initialized, err)
	}
	var migrated releasePointer
	if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Snapshot != originalReleaseSnapshot || migrated.HistorySnapshot == "" || migrated.HistorySnapshot == originalReleaseSnapshot || !migrated.UpdatedAt.Equal(legacyUpdatedAt) {
		t.Fatalf("migrated release pointer = %#v", migrated)
	}
	untouchedCollision, err := service.GetRevision("Post", post.Meta.ID, originalReleaseSnapshot)
	if err != nil || untouchedCollision.Content[post.Meta.SourceLocale].Title != "Unrelated history" {
		t.Fatalf("legacy history collision was overwritten: %#v, %v", untouchedCollision, err)
	}
	historyRelease, err := service.GetRevision("Post", post.Meta.ID, migrated.HistorySnapshot)
	if err != nil || historyRelease.Content[post.Meta.SourceLocale].Title != "Public" || historyRelease.Meta.Cover != "" {
		t.Fatalf("migrated release history = %#v, %v", historyRelease, err)
	}
	revisions, err := service.ListRevisions("Post", post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	releaseCount := 0
	for _, revision := range revisions {
		if revision.IsRelease {
			releaseCount++
			if revision.ID != migrated.HistorySnapshot {
				t.Fatalf("release history ID = %q, want %q", revision.ID, migrated.HistorySnapshot)
			}
		}
	}
	if releaseCount != 1 {
		t.Fatalf("release history count = %d, revisions = %#v", releaseCount, revisions)
	}
	beforeSecondInitialization, err := repository.ReadDir(paths.revisions)
	if err != nil {
		t.Fatal(err)
	}
	initialized, err = service.InitializePublishedReleases()
	if err != nil || initialized != 0 {
		t.Fatalf("second InitializePublishedReleases() = %d, %v", initialized, err)
	}
	var secondPointer releasePointer
	if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &secondPointer); err != nil {
		t.Fatal(err)
	}
	afterSecondInitialization, err := repository.ReadDir(paths.revisions)
	if err != nil {
		t.Fatal(err)
	}
	if secondPointer != migrated || len(afterSecondInitialization) != len(beforeSecondInitialization) {
		t.Fatalf("idempotent migration changed pointer/history: before=%#v/%d after=%#v/%d", migrated, len(beforeSecondInitialization), secondPointer, len(afterSecondInitialization))
	}
	secondPointer.HistorySnapshot = originalReleaseSnapshot
	if err := repository.WriteYAML(filepath.Join(root, "current.yaml"), secondPointer, false); err != nil {
		t.Fatal(err)
	}
	initialized, err = service.InitializePublishedReleases()
	if err != nil || initialized != 1 {
		t.Fatalf("repair InitializePublishedReleases() = %d, %v", initialized, err)
	}
	var repaired releasePointer
	if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &repaired); err != nil {
		t.Fatal(err)
	}
	if repaired.HistorySnapshot == originalReleaseSnapshot || repaired.Snapshot != originalReleaseSnapshot || !repaired.UpdatedAt.Equal(legacyUpdatedAt) {
		t.Fatalf("repaired release pointer = %#v", repaired)
	}
	repairedHistory, err := service.GetRevision("Post", post.Meta.ID, repaired.HistorySnapshot)
	if err != nil || repairedHistory.Content[post.Meta.SourceLocale].Title != "Public" {
		t.Fatalf("repaired release history = %#v, %v", repairedHistory, err)
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

func TestRestoreRevisionRemovesLocalesAbsentFromSnapshot(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			service, repository := testService(t)
			var item domain.Post
			var err error
			if kind == "Post" {
				item, err = service.CreatePost(CreatePostInput{ID: "restore-locales", Title: "源文", Markdown: "正文"})
			} else {
				item, err = service.CreatePage(CreatePageInput{ID: "restore-locales", Title: "源文", Markdown: "正文"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "Post" {
				item, err = service.UpdateLocale(item.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: item.Meta.Revision, Title: "English", Markdown: "Body"})
			} else {
				item, err = service.UpdatePageLocale(item.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: item.Meta.Revision, Title: "English", Markdown: "Body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			revisions, err := service.ListRevisions(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			var sourceOnlyRevision string
			for _, revision := range revisions {
				if revision.Revision == 1 {
					sourceOnlyRevision = revision.ID
					break
				}
			}
			if sourceOnlyRevision == "" {
				t.Fatal("source-only revision was not preserved")
			}
			restored, err := service.RestoreRevision(kind, item.Meta.ID, sourceOnlyRevision, item.Meta.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if _, exists := restored.Meta.Locales["en"]; exists {
				t.Fatalf("restored metadata retained removed locale: %#v", restored.Meta.Locales)
			}
			if _, exists := restored.Content["en"]; exists {
				t.Fatalf("restored content retained removed locale: %#v", restored.Content)
			}
			paths, err := lifecyclePaths(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repository.ReadFile(filepath.Join(paths.content, "en.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("removed head locale read error = %v, want not exist", err)
			}
			var reloaded domain.Post
			if kind == "Post" {
				reloaded, err = service.GetPost(item.Meta.ID)
			} else {
				reloaded, err = service.GetPage(item.Meta.ID)
			}
			if err != nil || reloaded.Meta.HeadRevision != restored.Meta.Revision {
				t.Fatalf("reloaded restored head = %#v, %v", reloaded.Meta, err)
			}
			revisions, err = service.ListRevisions(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			foundPreviousHead := false
			for _, revision := range revisions {
				if revision.Revision != item.Meta.Revision {
					continue
				}
				snapshot, err := service.GetRevision(kind, item.Meta.ID, revision.ID)
				if err != nil {
					t.Fatal(err)
				}
				_, foundPreviousHead = snapshot.Content["en"]
				if foundPreviousHead {
					break
				}
			}
			if !foundPreviousHead {
				t.Fatal("restore removed the locale from the historical snapshot")
			}
		})
	}
}

func TestRestoreRevisionKeepsCurrentScheduleIntentInsteadOfSnapshotIntent(t *testing.T) {
	service, _ := testService(t)
	post, err := service.CreatePost(CreatePostInput{ID: "restore-schedule-intent", Title: "First", Markdown: "One"})
	if err != nil {
		t.Fatal(err)
	}
	dueAt := time.Now().UTC().Add(time.Hour)
	visibility := post.Meta.Visibility
	post, err = service.UpdatePostSettings(post.Meta.ID, UpdatePostSettingsInput{
		ExpectedRevision: post.Meta.Revision, Categories: post.Meta.Categories, Tags: post.Meta.Tags,
		Pinned: &post.Meta.Pinned, Visibility: &visibility, PublishedAt: &dueAt, PublishTimeSet: true,
		CommentPolicy: post.Meta.CommentPolicy, Template: post.Meta.Template,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetScheduledPublish("Post", post.Meta.ID, post.Meta.Revision, dueAt); err != nil {
		t.Fatal(err)
	}
	oldScheduledRevision := post.Meta.Revision
	post, err = service.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, UpdateLocaleInput{
		ExpectedRevision: post.Meta.Revision, Title: "Second", Markdown: "Two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ClearScheduledPublish("Post", post.Meta.ID, oldScheduledRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetScheduledPublish("Post", post.Meta.ID, post.Meta.Revision, dueAt); err != nil {
		t.Fatal(err)
	}
	currentScheduledRevision := post.Meta.Revision

	revisions, err := service.ListRevisions("Post", post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldSnapshot := ""
	for _, revision := range revisions {
		if revision.Revision == oldScheduledRevision {
			oldSnapshot = revision.ID
			break
		}
	}
	if oldSnapshot == "" {
		t.Fatal("snapshot containing the old schedule intent was not found")
	}
	restored, err := service.RestoreRevision("Post", post.Meta.ID, oldSnapshot, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Meta.ScheduledRevision != currentScheduledRevision || restored.Meta.ScheduledRevision == oldScheduledRevision {
		t.Fatalf("restored schedule marker = %d, want current marker %d (not snapshot marker %d)", restored.Meta.ScheduledRevision, currentScheduledRevision, oldScheduledRevision)
	}
	if err := service.ClearScheduledPublish("Post", restored.Meta.ID, currentScheduledRevision); err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetPost(restored.Meta.ID)
	if err != nil || stored.Meta.ScheduledRevision != 0 {
		t.Fatalf("current schedule intent was not clearable after restore: %#v, %v", stored.Meta, err)
	}
}
