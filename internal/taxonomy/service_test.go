package taxonomy

import (
	"errors"
	"reflect"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestCategoryAndTagAreMultilingualFileTruth(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Enabled: true}, {Code: "en", Enabled: true}}}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	category, err := service.Create("Category", CreateInput{ID: "engineering", Name: "工程", Cover: "/media/2026/08/engineering.webp", Template: "masonry"})
	if err != nil {
		t.Fatal(err)
	}
	if category.Cover != "/media/2026/08/engineering.webp" || category.Template != "masonry" {
		t.Fatalf("category presentation settings = %#v", category)
	}
	if _, err := service.Create("Category", CreateInput{ID: "remote-cover", Name: "远程封面", Cover: "https://example.com/cover.webp"}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("remote category cover error = %v, want ErrInvalidSettings", err)
	}
	category, err = service.UpdateLocale("Category", category.ID, "en", UpdateLocaleInput{ExpectedRevision: category.Revision, Name: "Engineering"})
	if err != nil {
		t.Fatal(err)
	}
	if category.Locales["en"].Origin != domain.LocaleOriginManual {
		t.Fatalf("category = %#v", category)
	}
	child, err := service.Create("Category", CreateInput{ID: "backend", Name: "后端", ParentID: category.ID})
	if err != nil || child.ParentID != category.ID {
		t.Fatalf("child = %#v, err = %v", child, err)
	}
	tag, err := service.Create("Tag", CreateInput{ID: "release", Name: "发布"})
	if err != nil || tag.Kind != "Tag" {
		t.Fatalf("tag = %#v, err = %v", tag, err)
	}
	if items, err := service.List("Category"); err != nil || len(items) != 2 {
		t.Fatalf("categories = %#v, err = %v", items, err)
	}
	if _, err := service.UpdateStructure("Category", category.ID, UpdateStructureInput{ExpectedRevision: category.Revision, ParentID: child.ID, Cover: category.Cover, Template: category.Template}); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("category cycle error = %v, want ErrInvalidParent", err)
	}
	if err := service.Delete("Category", category.ID, category.Revision); !errors.Is(err, ErrInUse) {
		t.Fatalf("parent deletion error = %v, want ErrInUse", err)
	}
	child, err = service.UpdateStructure("Category", child.ID, UpdateStructureInput{ExpectedRevision: child.Revision})
	if err != nil || child.ParentID != "" {
		t.Fatalf("detach child = %#v, %v", child, err)
	}
	if err := service.Delete("Category", child.ID, child.Revision); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete("Category", category.ID, category.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get("Category", category.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted category error = %v", err)
	}
}

func TestTaxonomyIdentityCannotCrossFilePaths(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	first, err := service.Create("Category", CreateInput{ID: "first-category", Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create("Category", CreateInput{ID: "second-category", Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	first.ID = second.ID
	if err := repository.WriteYAML("content/taxonomies/categories/first-category.yaml", first, false); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete("Category", "first-category", first.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forged taxonomy delete error = %v", err)
	}
	if _, err := service.Get("Category", second.ID); err != nil {
		t.Fatalf("second taxonomy was affected: %v", err)
	}
}

func TestApplyAITranslationUsesSourceAndTargetLocaleRevisions(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Enabled: true}, {Code: "en", Enabled: true}}}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	item, err := service.Create("Category", CreateInput{ID: "engineering", Name: "工程", Description: "工程文章"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = service.ApplyAITranslation("Category", item.ID, "en", ApplyAITranslationInput{
		ExpectedSourceRevision: 1,
		ExpectedTargetRevision: 0,
		Name:                   " Engineering ",
		Description:            " Engineering posts ",
		SEOTitle:               " Engineering archive ",
		SEODescription:         " Posts about engineering ",
	})
	if err != nil {
		t.Fatal(err)
	}
	target := item.Locales["en"]
	if item.Revision != 2 || target.Name != "Engineering" || target.Description != "Engineering posts" || target.SEOTitle != "Engineering archive" || target.SEODescription != "Posts about engineering" {
		t.Fatalf("AI taxonomy = %#v", item)
	}
	if target.Revision != 1 || target.State != "current" || target.Origin != domain.LocaleOriginAI || target.SourceRevision != item.Locales[item.SourceLocale].Revision {
		t.Fatalf("AI taxonomy locale state = %#v", target)
	}

	before, err := service.Get("Category", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	conflicts := []struct {
		name  string
		input ApplyAITranslationInput
		want  error
	}{
		{name: "source changed", input: ApplyAITranslationInput{ExpectedSourceRevision: 2, ExpectedTargetRevision: 1, Name: "Changed"}, want: content.ErrSourceChanged},
		{name: "target changed", input: ApplyAITranslationInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Name: "Changed"}, want: content.ErrTargetChanged},
	}
	for _, test := range conflicts {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.ApplyAITranslation("Category", item.ID, "en", test.input); !errors.Is(err, test.want) {
				t.Fatalf("ApplyAITranslation() error = %v, want %v", err, test.want)
			}
			after, err := service.Get("Category", item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("conflicting AI write changed taxonomy: before=%#v after=%#v", before, after)
			}
		})
	}
	if _, err := service.ApplyAITranslation("Category", item.ID, "zh-CN", ApplyAITranslationInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 1, Name: "工程"}); !errors.Is(err, content.ErrLocaleDisabled) {
		t.Fatalf("source target error = %v, want ErrLocaleDisabled", err)
	}
	if _, err := service.ApplyAITranslation("Category", item.ID, "fr", ApplyAITranslationInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Name: "Ingénierie"}); !errors.Is(err, content.ErrLocaleDisabled) {
		t.Fatalf("disabled target error = %v, want ErrLocaleDisabled", err)
	}
}
