package content

import (
	"path/filepath"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestBuildSnapshotFallsBackToLastCompleteReleaseWhilePublicationIsPending(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			service, repository := testService(t)
			create := service.CreatePost
			publish := service.PublishPost
			apply := service.ApplyAITranslation
			update := service.UpdateLocale
			get := service.GetPost
			listForBuild := service.ListPostsForBuild
			listForLocalization := service.ListPostsForLocalization
			if kind == "Page" {
				create = service.CreatePage
				publish = service.PublishPage
				apply = service.ApplyAIPageTranslation
				update = service.UpdatePageLocale
				get = service.GetPage
				listForBuild = service.ListPagesForBuild
				listForLocalization = service.ListPagesForLocalization
			}

			item, err := create(CreatePostInput{ID: "fallback-release", Title: "First source", Markdown: "First body"})
			if err != nil {
				t.Fatal(err)
			}
			item, err = publish(item.Meta.ID, item.Meta.Revision)
			if err != nil {
				t.Fatal(err)
			}
			item, err = apply(item.Meta.ID, "en", ApplyAITranslationInput{
				ExpectedSourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision,
				Content:                domain.LocalizedMarkdown{Title: "First English", Markdown: "First English body"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.PromoteAITranslation(kind, item.Meta.ID, "en", item.Meta.Locales[item.Meta.SourceLocale].Revision); err != nil {
				t.Fatal(err)
			}
			item, err = get(item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			item, err = update(item.Meta.ID, item.Meta.SourceLocale, UpdateLocaleInput{
				ExpectedRevision: item.Meta.Revision,
				Title:            "Second source",
				Markdown:         "Second body",
			})
			if err != nil {
				t.Fatal(err)
			}
			item, err = publish(item.Meta.ID, item.Meta.Revision)
			if err != nil {
				t.Fatal(err)
			}

			pending, err := service.GetPublishedRelease(kind, item.Meta.ID)
			if err != nil || pending.Content[pending.Meta.SourceLocale].Title != "Second source" || pending.Meta.Locales["en"].State != "stale" {
				t.Fatalf("current pending release = %#v, %v", pending, err)
			}
			root, err := releaseRoot(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			var before releasePointer
			if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &before); err != nil {
				t.Fatal(err)
			}

			buildItems, err := listForBuild()
			if err != nil || len(buildItems) != 1 {
				t.Fatalf("build snapshot = %#v, %v", buildItems, err)
			}
			if got := buildItems[0].Content[buildItems[0].Meta.SourceLocale].Title; got != "First source" {
				t.Fatalf("build fallback source title = %q, want first complete release", got)
			}
			if got := buildItems[0].Content["en"].Title; got != "First English" {
				t.Fatalf("build fallback target title = %q, want first complete release", got)
			}
			localizationItems, err := listForLocalization()
			if err != nil || len(localizationItems) != 1 {
				t.Fatalf("localization snapshot = %#v, %v", localizationItems, err)
			}
			if got := localizationItems[0].Content[localizationItems[0].Meta.SourceLocale].Title; got != "Second source" {
				t.Fatalf("localization did not retain current pending release: %q", got)
			}
			var after releasePointer
			if err := repository.ReadYAML(filepath.Join(root, "current.yaml"), &after); err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("build fallback changed current release pointer: before=%#v after=%#v", before, after)
			}
		})
	}
}

func TestBuildSnapshotOmitsFirstPendingPublicationButLocalizationRetainsIt(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			service, _ := testService(t)
			create := service.CreatePost
			publish := service.PublishPost
			listForBuild := service.ListPostsForBuild
			listForLocalization := service.ListPostsForLocalization
			if kind == "Page" {
				create = service.CreatePage
				publish = service.PublishPage
				listForBuild = service.ListPagesForBuild
				listForLocalization = service.ListPagesForLocalization
			}
			item, err := create(CreatePostInput{ID: "first-pending", Title: "Pending source", Markdown: "Pending body"})
			if err != nil {
				t.Fatal(err)
			}
			item, err = publish(item.Meta.ID, item.Meta.Revision)
			if err != nil {
				t.Fatal(err)
			}
			buildItems, err := listForBuild()
			if err != nil || len(buildItems) != 0 {
				t.Fatalf("first pending publication leaked into build snapshot: %#v, %v", buildItems, err)
			}
			localizationItems, err := listForLocalization()
			if err != nil || len(localizationItems) != 1 || localizationItems[0].Content[localizationItems[0].Meta.SourceLocale].Title != "Pending source" {
				t.Fatalf("first pending localization snapshot = %#v, %v", localizationItems, err)
			}
		})
	}
}

func TestBuildSnapshotIncludesSourceWhenTargetLocaleIsNotVisible(t *testing.T) {
	service, repository := testService(t)
	hideTestTargetLocale(t, repository)
	post, err := service.CreatePost(CreatePostInput{ID: "hidden-target", Title: "Source only", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishPost(post.Meta.ID, post.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	items, err := service.ListPostsForBuild()
	if err != nil || len(items) != 1 || items[0].Content[items[0].Meta.SourceLocale].Title != "Source only" {
		t.Fatalf("source-only snapshot with hidden target = %#v, %v", items, err)
	}
}
