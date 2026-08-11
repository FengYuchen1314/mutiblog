package content

import (
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestCompleteScheduledPublishRestoresAutomaticTranslationPendingRelease(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		for _, releaseState := range []string{"missing-release", "same-revision-raw-release"} {
			t.Run(kind+"/"+releaseState, func(t *testing.T) {
				service, repository := testService(t)
				var (
					item domain.Post
					err  error
				)
				if kind == "Page" {
					item, err = service.CreatePage(CreatePageInput{ID: "scheduled-pending", Title: "源标题", Markdown: "源正文"})
				} else {
					item, err = service.CreatePost(CreatePostInput{ID: "scheduled-pending", Title: "源标题", Markdown: "源正文"})
				}
				if err != nil {
					t.Fatal(err)
				}

				translation := ApplyAITranslationInput{
					ExpectedSourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision,
					Content:                domain.LocalizedMarkdown{Title: "English title", Markdown: "English body"},
				}
				if kind == "Page" {
					item, err = service.ApplyAIPageTranslation(item.Meta.ID, "en", translation)
				} else {
					item, err = service.ApplyAITranslation(item.Meta.ID, "en", translation)
				}
				if err != nil {
					t.Fatal(err)
				}

				dueAt := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
				visibility := item.Meta.Visibility
				pinned := item.Meta.Pinned
				settings := UpdatePostSettingsInput{
					ExpectedRevision: item.Meta.Revision,
					Categories:       item.Meta.Categories,
					Tags:             item.Meta.Tags,
					Cover:            item.Meta.Cover,
					Pinned:           &pinned,
					Visibility:       &visibility,
					PublishedAt:      &dueAt,
					PublishTimeSet:   true,
					CommentPolicy:    item.Meta.CommentPolicy,
					Template:         item.Meta.Template,
				}
				if kind == "Page" {
					item, err = service.UpdatePageSettings(item.Meta.ID, settings)
				} else {
					item, err = service.UpdatePostSettings(item.Meta.ID, settings)
				}
				if err != nil {
					t.Fatal(err)
				}
				scheduledRevision := item.Meta.Revision
				item, err = service.SetScheduledPublish(kind, item.Meta.ID, scheduledRevision, dueAt)
				if err != nil {
					t.Fatal(err)
				}

				// This is the durable head transition a scheduled runner made before
				// the process stopped. The ordinary publication path leaves target
				// state current on the editable head but makes the public release stale.
				if kind == "Page" {
					item, err = service.PublishPage(item.Meta.ID, item.Meta.Revision)
				} else {
					item, err = service.PublishPost(item.Meta.ID, item.Meta.Revision)
				}
				if err != nil {
					t.Fatal(err)
				}
				headTarget := item.Meta.Locales["en"]
				if headTarget.State != "current" || headTarget.Origin != domain.LocaleOriginAI {
					t.Fatalf("published editable target = %#v", headTarget)
				}
				// A legacy interrupted head has no generation marker. A nonzero
				// marker, on the other hand, is already a durable task-ownership
				// identity and recovery must retain it.
				expectedGeneration := item.Meta.Revision
				if releaseState == "missing-release" {
					item.Meta.PublicationGeneration = 0
				} else {
					expectedGeneration += 17
					item.Meta.PublicationGeneration = expectedGeneration
				}
				metaPath := postPath(item.Meta.ID, "meta.yaml")
				if kind == "Page" {
					metaPath = pagePath(item.Meta.ID, "meta.yaml")
				}
				if err := repository.WriteYAML(metaPath, item.Meta, false); err != nil {
					t.Fatal(err)
				}

				root, err := releaseRoot(kind, item.Meta.ID)
				if err != nil {
					t.Fatal(err)
				}
				if releaseState == "missing-release" {
					if err := repository.RemoveTree(root); err != nil {
						t.Fatal(err)
					}
				} else {
					// InitializePublishedReleases can synthesize exactly this raw,
					// same-revision pointer after the crash. Its target looks current
					// unless CompleteScheduledPublish repairs it.
					if err := service.writeRelease(kind, item); err != nil {
						t.Fatal(err)
					}
					raw, err := service.GetPublishedRelease(kind, item.Meta.ID)
					if err != nil || raw.Meta.Revision != item.Meta.Revision || raw.Meta.PublicationGeneration != expectedGeneration || raw.Meta.Locales["en"].State != "current" {
						t.Fatalf("raw same-revision release = %#v, %v", raw, err)
					}
				}

				repaired, err := service.CompleteScheduledPublish(kind, item.Meta.ID, scheduledRevision, dueAt)
				if err != nil {
					t.Fatal(err)
				}
				if repaired.Meta.ReleaseRevision != repaired.Meta.Revision || repaired.Meta.PublicationGeneration != expectedGeneration || repaired.Meta.Locales["en"] != headTarget {
					t.Fatalf("repaired scheduled head = %#v", repaired)
				}
				released, err := service.GetPublishedRelease(kind, item.Meta.ID)
				if err != nil {
					t.Fatal(err)
				}
				target := released.Meta.Locales["en"]
				if released.Meta.Revision != repaired.Meta.Revision || released.Meta.PublicationGeneration != expectedGeneration || target.State != "stale" || target.Origin != domain.LocaleOriginAI || target.Revision != headTarget.Revision || target.SourceRevision != headTarget.SourceRevision || released.Content["en"] != item.Content["en"] {
					t.Fatalf("repaired automatic translation pending release = %#v", released)
				}
			})
		}
	}
}
