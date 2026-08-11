package themes

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func installThemeForTest(t *testing.T, service *Service, reader io.Reader) (View, error) {
	t.Helper()
	installation, err := service.BeginInstall(reader)
	if err != nil {
		return View{}, err
	}
	defer func() {
		if err := installation.Rollback(); err != nil {
			t.Errorf("rollback theme installation: %v", err)
		}
	}()
	if err := installation.Commit(); err != nil && !errors.Is(err, ErrCleanupPending) {
		return View{}, err
	}
	return installation.View, nil
}

func TestInstallActivateUpgradeAndUninstallTheme(t *testing.T) {
	service, repository := themeFixture(t)

	installed, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml":      "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\nassets: assets\nscreenshot: screenshot.webp\npostTemplates:\n  - id: gallery\n    name: Gallery\npageTemplates:\n  - id: landing\n    name: Landing\ncategoryTemplates:\n  - id: masonry\n    name: Masonry\n",
		"server.mjs":      "export const css = 'body{}';\n",
		"assets/app.js":   "console.log('theme');\n",
		"screenshot.webp": "preview",
	}))
	if err != nil {
		t.Fatalf("install theme: %v", err)
	}
	if installed.ID != "midnight" || installed.Active {
		t.Fatalf("unexpected installed theme: %#v", installed)
	}
	if installed.Status != "ready" || len(installed.Conditions) != 1 || !installed.Conditions[0].Status {
		t.Fatalf("unexpected theme status: %#v", installed)
	}
	if installed.ScreenshotURL != "/api/v1/admin/themes/midnight/screenshot?v=1.0.0" {
		t.Fatalf("unexpected screenshot URL: %q", installed.ScreenshotURL)
	}
	if screenshot, err := service.Screenshot("midnight"); err != nil || filepath.Base(screenshot) != "screenshot.webp" {
		t.Fatalf("screenshot = %q, %v", screenshot, err)
	}
	if err := service.Activate("midnight"); err != nil {
		t.Fatalf("activate theme: %v", err)
	}
	runtime, err := service.Runtime()
	if err != nil {
		t.Fatalf("read runtime: %v", err)
	}
	if runtime.ID != "midnight" || filepath.Base(runtime.ModulePath) != "server.mjs" || filepath.Base(runtime.AssetsPath) != "assets" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
	postTemplates, err := service.ActiveTemplates("post")
	if err != nil || len(postTemplates) != 2 || postTemplates[0].ID != "post" || postTemplates[1].ID != "gallery" {
		t.Fatalf("active post templates = %#v, %v", postTemplates, err)
	}
	if supported, err := service.SupportsActiveTemplate("page", "landing"); err != nil || !supported {
		t.Fatalf("landing template support = %v, %v", supported, err)
	}
	if supported, err := service.SupportsActiveTemplate("category", "masonry"); err != nil || !supported {
		t.Fatalf("masonry template support = %v, %v", supported, err)
	}
	if err := service.Uninstall("midnight"); !errors.Is(err, ErrActive) {
		t.Fatalf("active uninstall error = %v, want ErrActive", err)
	}

	upgraded, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 2.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = 'body{color:white}';\n",
	}))
	if err != nil {
		t.Fatalf("upgrade theme: %v", err)
	}
	if upgraded.Version != "2.0.0" || !upgraded.Active {
		t.Fatalf("unexpected upgraded theme: %#v", upgraded)
	}
	entries, err := os.ReadDir(filepath.Join(repository.Root(), "themes", "installed"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "midnight" {
			t.Fatalf("upgrade left temporary directory %q", entry.Name())
		}
	}

	if err := service.Activate("earth"); err != nil {
		t.Fatalf("restore built-in theme: %v", err)
	}
	if err := service.Uninstall("midnight"); err != nil {
		t.Fatalf("uninstall theme: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository.Root(), "themes", "installed", "midnight")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("theme directory still exists: %v", err)
	}
}

func TestInstallRejectsUnsafeArchives(t *testing.T) {
	service, repository := themeFixture(t)
	unsafe := []map[string]string{
		{"../escaped.txt": "nope"},
		{"theme.yaml": "schemaVersion: 1\nid: bad1\nname: Bad\nversion: 1\nengine: react-ssr\nserver: ../server.mjs\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad2\nname: Bad\nversion: 1\nengine: unsupported\nserver: server.mjs\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad3\nname: Bad\nversion: 1\nengine: react-ssr\nserver: server.mjs\npostTemplates:\n  - id: post\n    name: Reserved\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad4\nname: Bad\nversion: 1\nengine: react-ssr\nserver: server.mjs\npageTemplates:\n  - id: landing\n    name: First\n  - id: landing\n    name: Duplicate\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad5\nname: Bad\nversion: 1\nengine: react-ssr\nserver: server.mjs\nscreenshot: https://example.com/preview.png\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad6\nname: Bad\nversion: 1\nengine: react-ssr\nserver: server.mjs\nscreenshot: missing.png\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad7\nname: Bad\nversion: 1\nrequires: latest\nengine: react-ssr\nserver: server.mjs\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad8\nname: Bad\nversion: 1\nengine: react-ssr\nserver: server.mjs\nsettingsReload: hot\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: bad9\nname: Bad\nversion: 1\nengine: react-ssr\nserver: server.mjs\ncategoryTemplates:\n  - id: category\n    name: Reserved\n", "server.mjs": ""},
		{"theme.yaml": "schemaVersion: 1\nid: earth\nname: Shadow Earth\nversion: 1\nengine: react-ssr\nserver: server.mjs\n", "server.mjs": ""},
	}
	for index, files := range unsafe {
		if _, err := installThemeForTest(t, service, themeArchive(t, files)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("archive %d error = %v, want ErrInvalid", index, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repository.Root(), "themes", "escaped.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe archive escaped installation root: %v", err)
	}
}

func TestListRejectsThemeWhoseManifestIdentityDoesNotMatchItsDirectory(t *testing.T) {
	service, repository := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	})); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("themes/installed/midnight/theme.yaml", []byte("schemaVersion: 1\nid: forged\nname: Forged\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("List error = %v, want ErrInvalid", err)
	}
}

func TestListRejectsThemeWhoseAssetsWereReplacedWithSymlink(t *testing.T) {
	service, repository := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml":    "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\nassets: assets\n",
		"server.mjs":    "export const css = 'body{}';\n",
		"assets/app.js": "console.log('theme');\n",
	})); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(repository.Root(), "themes", "installed", "midnight", "assets", "outside")
	if err := os.Symlink(filepath.Join(repository.Root(), "config"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("List() error = %v, want ErrInvalid", err)
	}
}

func TestUninstallCanDeleteSavedSettings(t *testing.T) {
	service, repository := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: disposable\nname: Disposable\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	})); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML(settingsPath("disposable"), map[string]any{"color": "blue"}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML(localizedSettingsPath("disposable"), map[string]map[string]string{"fr": {"footer.title": "Titre"}}, false); err != nil {
		t.Fatal(err)
	}
	if err := service.UninstallWithSettings("disposable", true); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{settingsPath("disposable"), localizedSettingsPath("disposable")} {
		if _, err := repository.ReadFile(relative); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("saved settings still exist at %s: %v", relative, err)
		}
	}
}

func TestRecoverInterruptedThemeUninstall(t *testing.T) {
	service, repository := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: interrupted\nname: Interrupted\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	})); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML(settingsPath("interrupted"), map[string]any{"color": "blue"}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML(localizedSettingsPath("interrupted"), map[string]map[string]string{"fr": {"footer.title": "Titre"}}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile(deleteSettingsMarkerPath("interrupted"), []byte("pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repository.Root(), "themes", "installed", "interrupted")
	staging := filepath.Join(repository.Root(), "themes", "installed", ".uninstall-interrupted")
	if err := os.Rename(target, staging); err != nil {
		t.Fatal(err)
	}
	count, err := service.Recover()
	if err != nil || count != 2 {
		t.Fatalf("Recover uninstall = %d, %v", count, err)
	}
	for _, relative := range []string{settingsPath("interrupted"), localizedSettingsPath("interrupted"), deleteSettingsMarkerPath("interrupted")} {
		if _, err := repository.ReadFile(relative); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovered uninstall left %s: %v", relative, err)
		}
	}
}

func TestIncompatibleThemeCanBeInstalledButNotActivated(t *testing.T) {
	service, _ := themeFixture(t)
	installed, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: future\nname: Future\nversion: 1.0.0\nrequires: '>=9.0.0'\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if installed.Status != "incompatible" || installed.Conditions[0].Status || installed.Conditions[0].Reason != "RequirementNotSatisfied" {
		t.Fatalf("unexpected incompatible status: %#v", installed)
	}
	if err := service.Activate("future"); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("activate incompatible theme = %v, want ErrIncompatible", err)
	}
}

func TestThemeCompatibilityRequirements(t *testing.T) {
	tests := []struct {
		requirement string
		valid       bool
		satisfied   bool
	}{
		{"", true, true},
		{">=0.1.0 <1.0.0", true, true},
		{"0.1.0", true, true},
		{">0.1.0", true, false},
		{"<=0.0.9", true, false},
		{"^0.1.0", false, false},
		{"01.0.0", false, false},
	}
	for _, test := range tests {
		if got := validRequirement(test.requirement); got != test.valid {
			t.Errorf("validRequirement(%q) = %v, want %v", test.requirement, got, test.valid)
		}
		if got := requirementSatisfied(test.requirement, CompatibilityVersion); got != test.satisfied {
			t.Errorf("requirementSatisfied(%q) = %v, want %v", test.requirement, got, test.satisfied)
		}
	}
}

func TestRecoverInterruptedThemeInstall(t *testing.T) {
	service, repository := themeFixture(t)
	root := filepath.Join(repository.Root(), "themes", "installed")
	staging := filepath.Join(root, ".install-orphan")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(root, ".previous-midnight-1")
	if err := os.MkdirAll(previous, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "theme.yaml"), []byte("schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "server.mjs"), []byte("export const css = '';\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	count, err := service.Recover()
	if err != nil || count != 2 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan staging remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "midnight", "theme.yaml")); err != nil {
		t.Fatalf("previous theme was not restored: %v", err)
	}
}

func TestRecoverRollsBackUncommittedFreshThemeInstall(t *testing.T) {
	service, repository := themeFixture(t)
	installation, err := service.BeginInstall(themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: pending\nname: Pending\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const css = '';\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	count, err := NewService(repository).Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	if _, err := os.Stat(installation.target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncommitted fresh theme survived recovery: %v", err)
	}
	if _, err := repository.ReadFile(installation.markerRelative); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction marker survived recovery: %v", err)
	}
}

func TestRecoverRollsBackUncommittedThemeUpgrade(t *testing.T) {
	service, repository := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const version = 'old';\n",
	})); err != nil {
		t.Fatal(err)
	}
	installation, err := service.BeginInstall(themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 2.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const version = 'new';\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	count, err := NewService(repository).Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	manifest, err := service.readManifest("midnight")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.0.0" {
		t.Fatalf("recovered version = %q, want old package", manifest.Version)
	}
	if _, err := os.Stat(installation.backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("upgrade backup survived recovery: %v", err)
	}
}

func TestRecoverFinishesCommittedThemeUpgradeCleanup(t *testing.T) {
	service, repository := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 1.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const version = 'old';\n",
	})); err != nil {
		t.Fatal(err)
	}
	installation, err := service.BeginInstall(themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: midnight\nname: Midnight\nversion: 2.0.0\nengine: react-ssr\nserver: server.mjs\n",
		"server.mjs": "export const version = 'new';\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	transaction := installTransaction{
		SchemaVersion: domain.SchemaVersion,
		ThemeID:       installation.View.ID,
		Backup:        filepath.Base(installation.backup),
		State:         "committed",
	}
	if err := repository.WriteYAML(installation.markerRelative, transaction, false); err != nil {
		t.Fatal(err)
	}

	count, err := NewService(repository).Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	manifest, err := service.readManifest("midnight")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "2.0.0" {
		t.Fatalf("recovered version = %q, want committed package", manifest.Version)
	}
	for _, path := range []string{installation.backup, filepath.Join(repository.Root(), installation.markerRelative)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("committed cleanup left %s: %v", path, err)
		}
	}
}

func TestEarthSettingsUseDefaultsValidateAndReset(t *testing.T) {
	service, _ := themeFixture(t)
	settings, err := service.Settings("earth")
	if err != nil {
		t.Fatal(err)
	}
	style := settings.Values["style"].(map[string]any)
	sidebar := settings.Values["sidebar"].(map[string]any)
	widgets, widgetsOK := sidebar["widgets"].([]any)
	if style["accentColor"] != "#4ccba0" || !widgetsOK || len(widgets) != 3 || widgets[0] != "popular-posts" || widgets[1] != "categories" || widgets[2] != "tags" {
		t.Fatalf("default settings = %#v", settings.Values)
	}
	if _, err := service.SaveSettings("earth", map[string]any{"unknown": true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown setting error = %v, want ErrInvalid", err)
	}
	if _, err := service.SaveSettings("earth", map[string]any{"sidebar": map[string]any{"socialLinks": []any{map[string]any{"name": "Unsafe", "url": "https://example.com", "kind": "unsupported"}}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid nested array setting error = %v, want ErrInvalid", err)
	}
	updated, err := service.SaveSettings("earth", map[string]any{"style": map[string]any{"accentColor": "#ff0000"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Values["style"].(map[string]any)["accentColor"] != "#ff0000" || updated.Values["layout"] == nil {
		t.Fatalf("updated settings did not merge defaults: %#v", updated.Values)
	}
	updated, err = service.SaveSettings("earth", map[string]any{"sidebar": map[string]any{"widgets": []any{"tags", "profile"}, "socialLinks": []any{map[string]any{"name": "Example", "url": "https://example.com", "kind": "link"}}}})
	if err != nil || len(updated.Values["sidebar"].(map[string]any)["widgets"].([]any)) != 2 {
		t.Fatalf("save nested array settings = %#v, %v", updated.Values, err)
	}
	runtime, err := service.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Settings["style"].(map[string]any)["accentColor"] != "#ff0000" {
		t.Fatalf("runtime settings = %#v", runtime.Settings)
	}
	reset, err := service.ResetSettings("earth")
	if err != nil {
		t.Fatal(err)
	}
	if reset.Values["style"].(map[string]any)["accentColor"] != "#4ccba0" {
		t.Fatalf("reset settings = %#v", reset.Values)
	}
}

func TestEarthLocalizableSettingsAreExtractedValidatedAndLoaded(t *testing.T) {
	service, repository := themeFixture(t)
	_, err := service.SaveSettings("earth", map[string]any{
		"global": map[string]any{"brandSymbol": "B"},
		"layout": map[string]any{"heroKicker": "Earth stories"},
		"sidebar": map[string]any{"socialLinks": []any{
			map[string]any{"name": "Community", "url": "https://example.com/community", "kind": "link"},
			map[string]any{"name": "", "url": "https://example.com/unnamed", "kind": "link"},
		}},
		"footer": map[string]any{
			"title":     "Stay curious",
			"slogan":    "Stories from everywhere",
			"copyright": "All rights reserved",
			"socialLinks": []any{
				map[string]any{"name": "Follow us", "url": "https://example.com/follow", "kind": "link"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	source, err := service.LocalizableText("earth")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"layout.heroKicker":          "Earth stories",
		"sidebar.socialLinks.0.name": "Community",
		"footer.title":               "Stay curious",
		"footer.slogan":              "Stories from everywhere",
		"footer.copyright":           "All rights reserved",
		"footer.socialLinks.0.name":  "Follow us",
	}
	if len(source) != len(want) {
		t.Fatalf("localizable settings = %#v, want %#v", source, want)
	}
	for path, expected := range want {
		if source[path] != expected {
			t.Errorf("localizable setting %q = %q, want %q", path, source[path], expected)
		}
	}
	if _, exists := source["global.brandSymbol"]; exists {
		t.Fatalf("brand symbol must not be localizable: %#v", source)
	}
	if _, exists := source["sidebar.socialLinks.1.name"]; exists {
		t.Fatalf("empty social-link name must not be localizable: %#v", source)
	}

	translations := make(map[string]string, len(source))
	for path, value := range source {
		translations[path] = "Translated " + value
	}
	clone := func(values map[string]string) map[string]string {
		copy := make(map[string]string, len(values))
		for path, value := range values {
			copy[path] = value
		}
		return copy
	}
	missing := clone(translations)
	delete(missing, "footer.title")
	if err := service.WriteLocalizedText("earth", "en-US", missing); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing localized setting error = %v, want ErrInvalid", err)
	}
	extra := clone(translations)
	extra["footer.unknown"] = "Unexpected"
	if err := service.WriteLocalizedText("earth", "en-US", extra); !errors.Is(err, ErrInvalid) {
		t.Fatalf("extra localized setting error = %v, want ErrInvalid", err)
	}
	blank := clone(translations)
	blank["footer.title"] = "   "
	if err := service.WriteLocalizedText("earth", "en-US", blank); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank localized setting error = %v, want ErrInvalid", err)
	}
	if err := service.WriteLocalizedText("earth", "not a locale", translations); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid locale error = %v, want ErrInvalid", err)
	}
	if err := service.WriteLocalizedText("earth", "zh-CN", translations); !errors.Is(err, ErrInvalid) {
		t.Fatalf("source locale write error = %v, want ErrInvalid", err)
	}
	if err := service.WriteLocalizedText("earth", "en-us", translations); err != nil {
		t.Fatal(err)
	}

	var stored map[string]map[string]string
	if err := repository.ReadYAML(localizedSettingsPath("earth"), &stored); err != nil {
		t.Fatal(err)
	}
	if stored["en-US"]["footer.title"] != translations["footer.title"] {
		t.Fatalf("stored localized settings = %#v", stored)
	}
	runtime, err := service.RuntimeFor("earth")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.LocalizedSettings["en-US"]["layout.heroKicker"] != translations["layout.heroKicker"] {
		t.Fatalf("runtime localized settings = %#v", runtime.LocalizedSettings)
	}
}

func TestCustomThemeWithoutLocalizableContractIsNotLocalized(t *testing.T) {
	service, _ := themeFixture(t)
	if _, err := installThemeForTest(t, service, themeArchive(t, map[string]string{
		"theme.yaml": "schemaVersion: 1\nid: plain\nname: Plain\nversion: 1.0.0\nengine: react-ssr\n" +
			"server: server.mjs\nsettingsSchema: settings.schema.json\n",
		"server.mjs":           "export const css = '';\n",
		"settings.schema.json": `{"type":"object","properties":{"headline":{"type":"string","default":"Custom headline"}}}`,
	})); err != nil {
		t.Fatal(err)
	}
	values, err := service.LocalizableText("plain")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("custom theme without x-localizable contract returned %#v", values)
	}
	if err := service.WriteLocalizedText("plain", "en", map[string]string{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("custom theme localized write error = %v, want ErrInvalid", err)
	}
}

func themeFixture(t *testing.T) (*Service, *fsrepo.Repository) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{
		SchemaVersion: 1,
		SourceLocale:  "zh-CN",
		AdminLocale:   "zh-CN",
		Timezone:      "Asia/Shanghai",
		ActiveTheme:   "earth",
		Locales:       map[string]domain.LocalizedSite{"zh-CN": {Title: "Test"}},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, false); err != nil {
		t.Fatal(err)
	}
	return NewService(repository), repository
}

func themeArchive(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buffer.Bytes())
}
