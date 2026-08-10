package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/config"
	"github.com/FengYuchen1314/mutiblog/internal/model"
)

func TestActivateReleaseMigratesDirectoryAndSwitchesSymlink(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "generated", "public")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	releases := releaseRoot(output)
	staging := filepath.Join(releases, "release-2")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "index.html"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := activateRelease(output, staging, releases, "2"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(output)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("published output is not a symlink: %v, %#v", err, info)
	}
	got, err := os.ReadFile(filepath.Join(output, "index.html"))
	if err != nil || string(got) != "new" {
		t.Fatalf("active release = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(releases, "legacy-2", "index.html")); err != nil {
		t.Fatalf("previous directory was not retained for rollback: %v", err)
	}

	// A release count below the retention threshold must be a no-op (and used
	// to panic due to slicing past the end of the directory list).
	pruneReleases(releases, 3)
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("current release was pruned: %v", err)
	}
}

func TestTranslationTargetsUseOnlyConfiguredEnabledLocales(t *testing.T) {
	instance := &App{Config: &config.Config{I18n: config.I18nConfig{
		DefaultLocale: "zh-CN", SourceLocale: "zh-CN",
		Locales: []config.LocaleConfig{
			{Code: "zh-CN", URLPrefix: "zh-cn", Enabled: true},
			{Code: "en", URLPrefix: "en", Enabled: true},
			{Code: "ja", URLPrefix: "ja", Enabled: true},
			{Code: "de", URLPrefix: "de", Enabled: false},
		},
		TranslateTargets: []string{"en", "ja", "en", "de", "unknown"},
	}}}
	got := instance.translationTargets(model.Locale("zh-CN"))
	if len(got) != 2 || got[0] != "en" || got[1] != "ja" {
		t.Fatalf("targets=%v", got)
	}
	instance.Config.I18n.TranslateTargets = nil
	got = instance.translationTargets(model.Locale("zh-CN"))
	if len(got) != 2 || got[0] != "en" || got[1] != "ja" {
		t.Fatalf("default targets=%v", got)
	}
}

func TestPaginationUsesCanonicalLocalePaths(t *testing.T) {
	pagination := paginationFor("zh-cn", 2, 3)
	if pagination.Page != 2 || pagination.Total != 3 || len(pagination.Links) != 3 {
		t.Fatalf("pagination=%#v", pagination)
	}
	if pagination.Links[0].URL != "/zh-cn/" || pagination.Links[1].URL != "/zh-cn/page/2/" ||
		!pagination.Links[1].Current {
		t.Fatalf("links=%#v", pagination.Links)
	}
}
