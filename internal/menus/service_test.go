package menus

import (
	"errors"
	"reflect"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestNestedMultilingualMenu(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Enabled: true}, {Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	menu, err := service.Create(CreateInput{ID: "primary", Label: "主菜单"})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.AddItem(menu.ID, AddItemInput{ID: "about", TargetKind: "internal", URL: "/pages/about-site/", Label: "关于", ExpectedRevision: menu.Revision})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.AddItem(menu.ID, AddItemInput{ID: "source-code", ParentID: "about", TargetKind: "external", URL: "https://github.com/example/project", Label: "源码", ExpectedRevision: menu.Revision})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.UpdateItemLocale(menu.ID, "about", "en", UpdateLocaleInput{ExpectedRevision: menu.Revision, Label: "About"})
	if err != nil || menu.Items[0].Locales["en"].Origin != domain.LocaleOriginManual {
		t.Fatalf("menu=%#v err=%v", menu, err)
	}
	if _, err := service.AddItem(menu.ID, AddItemInput{TargetKind: "internal", URL: "//evil.example", Label: "Bad", ExpectedRevision: menu.Revision}); err == nil {
		t.Fatal("unsafe internal target accepted")
	}
	if _, err := service.UpdateItem(menu.ID, "about", UpdateItemInput{ExpectedRevision: menu.Revision, ParentID: "source-code", TargetKind: "internal", URL: "/about/"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("menu cycle error = %v, want ErrInvalid", err)
	}
	menu, err = service.UpdateItem(menu.ID, "source-code", UpdateItemInput{ExpectedRevision: menu.Revision, ParentID: "about", TargetKind: "external", URL: "https://github.com/example/project", OpenInNew: true, Order: 7})
	if err != nil || !menu.Items[1].OpenInNew || menu.Items[1].Order != 7 {
		t.Fatalf("menu item structure = %#v, %v", menu, err)
	}
	if _, err := service.DeleteItem(menu.ID, "about", menu.Revision); !errors.Is(err, ErrInUse) {
		t.Fatalf("parent menu item deletion error = %v, want ErrInUse", err)
	}
	menu, err = service.DeleteItem(menu.ID, "source-code", menu.Revision)
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.DeleteItem(menu.ID, "about", menu.Revision)
	if err != nil || len(menu.Items) != 0 {
		t.Fatalf("delete parent item = %#v, %v", menu, err)
	}
	if err := service.Delete(menu.ID, menu.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(menu.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted menu error = %v", err)
	}
}

func TestMenuIdentityCannotCrossFilePaths(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	first, err := service.Create(CreateInput{ID: "first-menu", Label: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(CreateInput{ID: "second-menu", Label: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	first.ID = second.ID
	if err := repository.WriteYAML("content/menus/first-menu.yaml", first, false); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete("first-menu", first.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forged menu delete error = %v", err)
	}
	if _, err := service.Get(second.ID); err != nil {
		t.Fatalf("second menu was affected: %v", err)
	}
}

func TestMenuGetRejectsUnsafeTargetFromExternalEdit(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	menu, err := service.Create(CreateInput{ID: "primary-menu", Label: "Primary"})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.AddItem(menu.ID, AddItemInput{ID: "home-link", TargetKind: "internal", URL: "/", Label: "Home", ExpectedRevision: menu.Revision})
	if err != nil {
		t.Fatal(err)
	}
	menu.Items[0].TargetKind = "external"
	menu.Items[0].URL = "javascript:alert(1)"
	if err := repository.WriteYAML("content/menus/primary-menu.yaml", menu, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(menu.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Get error = %v, want ErrInvalid", err)
	}
}

func TestApplyAILocalesUseSourceAndTargetLocaleRevisions(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Enabled: true}, {Code: "en", Enabled: true}}}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	menu, err := service.Create(CreateInput{ID: "primary", Label: "主菜单"})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.AddItem(menu.ID, AddItemInput{ID: "about", TargetKind: "internal", URL: "/pages/about-site/", Label: "关于", ExpectedRevision: menu.Revision})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = service.ApplyAIMenuLocale(menu.ID, "en", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Label: " Primary "})
	if err != nil {
		t.Fatal(err)
	}
	menuTarget := menu.Locales["en"]
	if menu.Revision != 3 || menuTarget.Label != "Primary" || menuTarget.Revision != 1 || menuTarget.State != "current" || menuTarget.Origin != domain.LocaleOriginAI || menuTarget.SourceRevision != 1 {
		t.Fatalf("AI menu = %#v", menu)
	}
	menu, err = service.ApplyAIItemLocale(menu.ID, "about", "en", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Label: " About "})
	if err != nil {
		t.Fatal(err)
	}
	itemTarget := menu.Items[0].Locales["en"]
	if menu.Revision != 4 || itemTarget.Label != "About" || itemTarget.Revision != 1 || itemTarget.State != "current" || itemTarget.Origin != domain.LocaleOriginAI || itemTarget.SourceRevision != 1 {
		t.Fatalf("AI menu item = %#v", menu)
	}

	before, err := service.Get(menu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAIMenuLocale(menu.ID, "en", ApplyAILocaleInput{ExpectedSourceRevision: 2, ExpectedTargetRevision: 1, Label: "Changed"}); !errors.Is(err, content.ErrSourceChanged) {
		t.Fatalf("changed menu source error = %v, want ErrSourceChanged", err)
	}
	after, err := service.Get(menu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("source conflict changed menu: before=%#v after=%#v", before, after)
	}
	if _, err := service.ApplyAIItemLocale(menu.ID, "about", "en", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Label: "Changed"}); !errors.Is(err, content.ErrTargetChanged) {
		t.Fatalf("changed item target error = %v, want ErrTargetChanged", err)
	}
	after, err = service.Get(menu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("target conflict changed menu: before=%#v after=%#v", before, after)
	}
	if _, err := service.ApplyAIMenuLocale(menu.ID, "zh-CN", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 1, Label: "主菜单"}); !errors.Is(err, content.ErrLocaleDisabled) {
		t.Fatalf("source menu target error = %v, want ErrLocaleDisabled", err)
	}
	if _, err := service.ApplyAIItemLocale(menu.ID, "about", "fr", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Label: "À propos"}); !errors.Is(err, content.ErrLocaleDisabled) {
		t.Fatalf("disabled item target error = %v, want ErrLocaleDisabled", err)
	}
}
