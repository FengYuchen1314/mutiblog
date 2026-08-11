package localeconfig

import (
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestMigrateFallbackRestoresConfirmedOrderOnce(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled:       []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}},
		Fallback:      []string{"en", "zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", legacy, false); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateFallback(repository)
	if err != nil || !changed {
		t.Fatalf("first migration = %v, %v; want changed", changed, err)
	}
	var migrated domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &migrated); err != nil {
		t.Fatal(err)
	}
	if !sameStrings(migrated.Fallback, FixedFallbackOrder()) || migrated.SourceLocale != legacy.SourceLocale || len(migrated.Enabled) != 2 {
		t.Fatalf("migrated locales = %#v", migrated)
	}
	if !migrated.Enabled[0].Enabled || migrated.Enabled[0].Code != "en" {
		t.Fatalf("existing source locale was changed = %#v", migrated.Enabled[0])
	}
	chinese := migrated.Enabled[1]
	if chinese.Code != DefaultFallback || chinese.Label != DefaultFallbackLabel || !chinese.Enabled {
		t.Fatalf("migrated Chinese fallback = %#v", chinese)
	}
	changed, err = MigrateFallback(repository)
	if err != nil || changed {
		t.Fatalf("second migration = %v, %v; want no change", changed, err)
	}
}

func TestNormalizeCanonicalizesEnablesAndDeduplicatesRequiredChineseFallback(t *testing.T) {
	config := domain.LocalesConfig{
		Fallback: []string{"en", DefaultFallback},
		Enabled: []domain.LocaleDefinition{
			{Code: "en", Label: "English", Enabled: false},
			{Code: "zh-cn", Label: "中文", Enabled: false},
			{Code: DefaultFallback, Label: "重复项", Enabled: true},
		},
	}
	if !Normalize(&config) {
		t.Fatal("expected legacy safety locale definitions to be normalized")
	}
	if len(config.Enabled) != 2 || !sameStrings(config.Fallback, FixedFallbackOrder()) {
		t.Fatalf("normalized definitions = %#v", config.Enabled)
	}
	english := config.Enabled[0]
	if english.Code != "en" || english.Label != "English" || english.Enabled {
		t.Fatalf("ordinary English target was unexpectedly forced = %#v", english)
	}
	chinese := config.Enabled[1]
	if chinese.Code != DefaultFallback || chinese.Label != "中文" || !chinese.Enabled {
		t.Fatalf("normalized Chinese fallback = %#v", chinese)
	}
	if Normalize(&config) {
		t.Fatal("normalized configuration should be idempotent")
	}
}

func TestMigrateFallbackSkipsUninitializedRepository(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateFallback(repository)
	if err != nil || changed {
		t.Fatalf("uninitialized migration = %v, %v", changed, err)
	}
}
