package content

import (
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestPublishMarksEveryDerivedReleaseTargetPendingWithoutMutatingHead(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		for _, origin := range []domain.LocaleOrigin{domain.LocaleOriginAI, domain.LocaleOriginManual} {
			t.Run(kind+"/"+string(origin), func(t *testing.T) {
				service, repository := testService(t)
				var item domain.Post
				var err error
				if kind == "Page" {
					item, err = service.CreatePage(CreatePageInput{ID: "manual-publish-pending", Title: "源标题", Markdown: "源正文"})
				} else {
					item, err = service.CreatePost(CreatePostInput{ID: "manual-publish-pending", Title: "源标题", Markdown: "源正文"})
				}
				if err != nil {
					t.Fatal(err)
				}
				if kind == "Page" {
					item, err = service.PublishPage(item.Meta.ID, item.Meta.Revision)
				} else {
					item, err = service.PublishPost(item.Meta.ID, item.Meta.Revision)
				}
				if err != nil {
					t.Fatal(err)
				}
				localized := domain.LocalizedMarkdown{Title: "AI target", Markdown: "AI body"}
				if kind == "Page" {
					item, err = service.ApplyAIPageTranslation(item.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision, Content: localized})
				} else {
					item, err = service.ApplyAITranslation(item.Meta.ID, "en", ApplyAITranslationInput{ExpectedSourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision, Content: localized})
				}
				if err != nil {
					t.Fatal(err)
				}
				metaPath := postPath(item.Meta.ID, "meta.yaml")
				if kind == "Page" {
					metaPath = pagePath(item.Meta.ID, "meta.yaml")
				}
				if origin == domain.LocaleOriginManual {
					// Imported legacy repositories may still contain a manual target. The
					// editor can no longer create one, but publish migration must continue
					// to handle the stored origin safely until AI replaces it.
					manualState := item.Meta.Locales["en"]
					manualState.Origin = domain.LocaleOriginManual
					item.Meta.Locales["en"] = manualState
					if err := repository.WriteYAML(metaPath, item.Meta, false); err != nil {
						t.Fatal(err)
					}
				}
				promote := service.PromoteAITranslation
				if origin == domain.LocaleOriginManual {
					promote = service.PromoteCurrentTranslation
				}
				if err := promote(kind, item.Meta.ID, "en", item.Meta.Locales[item.Meta.SourceLocale].Revision); err != nil {
					t.Fatal(err)
				}
				historical, err := service.GetPublishedRelease(kind, item.Meta.ID)
				if err != nil {
					t.Fatal(err)
				}
				historicalState := historical.Meta.Locales["en"]
				if historicalState.Origin != origin || historicalState.State != "current" {
					t.Fatalf("historical release state = %#v", historicalState)
				}
				if origin == domain.LocaleOriginAI {
					// A direct Git/filesystem edit can alter the target file without
					// changing its trusted-looking AI metadata. Publishing must still
					// force a fresh AI result instead of accepting this content as current.
					localized = domain.LocalizedMarkdown{Title: "Externally edited target", Markdown: "Not an AI result"}
					if kind == "Page" {
						err = service.writePageLocale(item.Meta.ID, "en", localized)
					} else {
						err = service.writeLocale(item.Meta.ID, "en", localized)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if kind == "Page" {
					item, err = service.GetPage(item.Meta.ID)
				} else {
					item, err = service.GetPost(item.Meta.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				beforePublishState := item.Meta.Locales["en"]
				beforePublishContent := item.Content["en"]
				if beforePublishState.Origin != origin || beforePublishState.State != "current" || beforePublishContent != localized {
					t.Fatalf("head before publish = %#v", item)
				}
				if kind == "Page" {
					item, err = service.PublishPage(item.Meta.ID, item.Meta.Revision)
				} else {
					item, err = service.PublishPost(item.Meta.ID, item.Meta.Revision)
				}
				if err != nil {
					t.Fatal(err)
				}
				headState := item.Meta.Locales["en"]
				if headState != beforePublishState || item.Content["en"] != beforePublishContent {
					t.Fatalf("published editable head target changed = %#v", item)
				}
				released, err := service.GetPublishedRelease(kind, item.Meta.ID)
				if err != nil {
					t.Fatal(err)
				}
				releaseState := released.Meta.Locales["en"]
				if releaseState.Origin != origin || releaseState.State != "stale" || releaseState.Revision != headState.Revision || releaseState.SourceRevision != headState.SourceRevision || released.Content["en"] != item.Content["en"] {
					t.Fatalf("automatic translation pending release = %#v", released)
				}
			})
		}
	}
}
