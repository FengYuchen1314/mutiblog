package links

import (
	"errors"
	"reflect"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestMultilingualLinkLifecycle(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Enabled: true}, {Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	group, err := service.CreateGroup(CreateGroupInput{ID: "friends", Name: "朋友"})
	if err != nil {
		t.Fatal(err)
	}
	group, err = service.UpdateGroupLocale(group.ID, "en", UpdateLocaleInput{ExpectedRevision: group.Revision, Name: "Friends"})
	if err != nil || group.Locales["en"].Origin != domain.LocaleOriginManual {
		t.Fatalf("group=%#v err=%v", group, err)
	}
	link, err := service.CreateLink(CreateLinkInput{ID: "example-site", GroupID: group.ID, URL: "https://example.com/path", Name: "示例"})
	if err != nil {
		t.Fatal(err)
	}
	link, err = service.UpdateLinkLocale(link.ID, "en", UpdateLocaleInput{ExpectedRevision: link.Revision, Name: "Example"})
	if err != nil || link.Locales["en"].Name != "Example" {
		t.Fatalf("link=%#v err=%v", link, err)
	}
	if _, err := service.CreateLink(CreateLinkInput{GroupID: group.ID, URL: "https://user:pass@example.com", Name: "Bad"}); err == nil {
		t.Fatal("URL credentials accepted")
	}
	group, err = service.UpdateGroup(group.ID, UpdateGroupInput{ExpectedRevision: group.Revision, Order: 4})
	if err != nil || group.Order != 4 {
		t.Fatalf("group order = %#v, %v", group, err)
	}
	link, err = service.UpdateLink(link.ID, UpdateLinkInput{ExpectedRevision: link.Revision, GroupID: group.ID, URL: "https://example.com/updated", Logo: "/media/logo.webp", Order: 2})
	if err != nil || link.Order != 2 || link.URL != "https://example.com/updated" {
		t.Fatalf("link structure = %#v, %v", link, err)
	}
	if err := service.DeleteGroup(group.ID, group.Revision); !errors.Is(err, ErrInUse) {
		t.Fatalf("non-empty group deletion error = %v, want ErrInUse", err)
	}
	if err := service.DeleteLink(link.ID, link.Revision); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteGroup(group.ID, group.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestLinkIdentityCannotCrossFilePaths(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	group, err := service.CreateGroup(CreateGroupInput{ID: "friends", Name: "Friends"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateLink(CreateLinkInput{ID: "first-link", GroupID: group.ID, URL: "https://first.example", Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateLink(CreateLinkInput{ID: "second-link", GroupID: group.ID, URL: "https://second.example", Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	first.ID = second.ID
	if err := repository.WriteYAML("content/links/items/first-link.yaml", first, false); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteLink("first-link", first.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forged link delete error = %v", err)
	}
	if _, err := service.getLink(second.ID); err != nil {
		t.Fatalf("second link was affected: %v", err)
	}
}

func TestLinkListRejectsUnsafeURLFromExternalEdit(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Enabled: true}}}, false); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	group, err := service.CreateGroup(CreateGroupInput{ID: "friends", Name: "Friends"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := service.CreateLink(CreateLinkInput{ID: "safe-link", GroupID: group.ID, URL: "https://example.com", Name: "Safe"})
	if err != nil {
		t.Fatal(err)
	}
	link.URL = "javascript:alert(1)"
	if err := repository.WriteYAML("content/links/items/safe-link.yaml", link, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListLinks(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ListLinks error = %v, want ErrInvalid", err)
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
	group, err := service.CreateGroup(CreateGroupInput{ID: "friends", Name: "朋友", Description: "友情链接"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := service.CreateLink(CreateLinkInput{ID: "example-site", GroupID: group.ID, URL: "https://example.com", Name: "示例", Description: "示例站点"})
	if err != nil {
		t.Fatal(err)
	}
	group, err = service.ApplyAIGroupLocale(group.ID, "en", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Name: " Friends ", Description: " Friendly sites "})
	if err != nil {
		t.Fatal(err)
	}
	groupTarget := group.Locales["en"]
	if group.Revision != 2 || groupTarget.Name != "Friends" || groupTarget.Description != "Friendly sites" || groupTarget.Revision != 1 || groupTarget.State != "current" || groupTarget.Origin != domain.LocaleOriginAI || groupTarget.SourceRevision != 1 {
		t.Fatalf("AI link group = %#v", group)
	}
	link, err = service.ApplyAILinkLocale(link.ID, "en", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Name: " Example ", Description: " Example site "})
	if err != nil {
		t.Fatal(err)
	}
	linkTarget := link.Locales["en"]
	if link.Revision != 2 || linkTarget.Name != "Example" || linkTarget.Description != "Example site" || linkTarget.Revision != 1 || linkTarget.State != "current" || linkTarget.Origin != domain.LocaleOriginAI || linkTarget.SourceRevision != 1 {
		t.Fatalf("AI link = %#v", link)
	}

	beforeGroup, err := service.getGroup(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAIGroupLocale(group.ID, "en", ApplyAILocaleInput{ExpectedSourceRevision: 2, ExpectedTargetRevision: 1, Name: "Changed"}); !errors.Is(err, content.ErrSourceChanged) {
		t.Fatalf("changed group source error = %v, want ErrSourceChanged", err)
	}
	afterGroup, err := service.getGroup(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterGroup, beforeGroup) {
		t.Fatalf("source conflict changed group: before=%#v after=%#v", beforeGroup, afterGroup)
	}

	beforeLink, err := service.getLink(link.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAILinkLocale(link.ID, "en", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Name: "Changed"}); !errors.Is(err, content.ErrTargetChanged) {
		t.Fatalf("changed link target error = %v, want ErrTargetChanged", err)
	}
	afterLink, err := service.getLink(link.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterLink, beforeLink) {
		t.Fatalf("target conflict changed link: before=%#v after=%#v", beforeLink, afterLink)
	}
	if _, err := service.ApplyAIGroupLocale(group.ID, "zh-CN", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 1, Name: "朋友"}); !errors.Is(err, content.ErrLocaleDisabled) {
		t.Fatalf("source group target error = %v, want ErrLocaleDisabled", err)
	}
	if _, err := service.ApplyAILinkLocale(link.ID, "fr", ApplyAILocaleInput{ExpectedSourceRevision: 1, ExpectedTargetRevision: 0, Name: "Exemple"}); !errors.Is(err, content.ErrLocaleDisabled) {
		t.Fatalf("disabled link target error = %v, want ErrLocaleDisabled", err)
	}
}
