package links

import (
	"errors"
	"testing"

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
