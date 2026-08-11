package content

import (
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestPublishMarksCurrentManualReleaseTargetPendingWithoutMutatingHead(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			service, _ := testService(t)
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
			if kind == "Page" {
				item, err = service.UpdatePageLocale(item.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: item.Meta.Revision, Title: "Manual", Markdown: "Manual body"})
			} else {
				item, err = service.UpdateLocale(item.Meta.ID, "en", UpdateLocaleInput{ExpectedRevision: item.Meta.Revision, Title: "Manual", Markdown: "Manual body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := service.PromoteCurrentTranslation(kind, item.Meta.ID, "en", item.Meta.Locales[item.Meta.SourceLocale].Revision); err != nil {
				t.Fatal(err)
			}
			historical, err := service.GetPublishedRelease(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			historicalState := historical.Meta.Locales["en"]
			if historicalState.Origin != domain.LocaleOriginManual || historicalState.State != "current" {
				t.Fatalf("historical manual release state = %#v", historicalState)
			}
			if kind == "Page" {
				item, err = service.GetPage(item.Meta.ID)
			} else {
				item, err = service.GetPost(item.Meta.ID)
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
			headState := item.Meta.Locales["en"]
			if headState.Origin != domain.LocaleOriginManual || headState.State != "current" {
				t.Fatalf("published editable head state = %#v", headState)
			}
			released, err := service.GetPublishedRelease(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			releaseState := released.Meta.Locales["en"]
			if releaseState.Origin != domain.LocaleOriginManual || releaseState.State != "stale" || releaseState.Revision != headState.Revision || releaseState.SourceRevision != headState.SourceRevision || released.Content["en"] != item.Content["en"] {
				t.Fatalf("automatic translation pending release = %#v", released)
			}
		})
	}
}
