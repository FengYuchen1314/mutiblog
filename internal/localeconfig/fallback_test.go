package localeconfig

import (
	"testing"
	"time"

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
	legacySite := domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Locales:       map[string]domain.LocalizedSite{"en": {Title: "Legacy site"}},
		UpdatedAt:     time.Now().UTC().Add(-time.Hour),
	}
	if err := repository.WriteYAML("config/locales.yaml", legacy, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", legacySite, false); err != nil {
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
	if !sameStrings(migrated.Fallback, FixedFallbackOrder()) || migrated.SourceLocale != FixedSourceLocale || len(migrated.Enabled) != 2 {
		t.Fatalf("migrated locales = %#v", migrated)
	}
	if !migrated.Enabled[0].Enabled || migrated.Enabled[0].Code != "en" || migrated.Enabled[0].Status != "" {
		t.Fatalf("existing source locale was changed = %#v", migrated.Enabled[0])
	}
	chinese := migrated.Enabled[1]
	if chinese.Code != DefaultFallback || chinese.Label != DefaultFallbackLabel || !chinese.Enabled || chinese.Status != domain.LocaleStatusReady {
		t.Fatalf("migrated Chinese fallback = %#v", chinese)
	}
	var migratedSite domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &migratedSite); err != nil {
		t.Fatal(err)
	}
	if migratedSite.SourceLocale != FixedSourceLocale || migratedSite.Locales[FixedSourceLocale].Title != "Legacy site" || migratedSite.Locales["en"].Title != "Legacy site" {
		t.Fatalf("migrated site = %#v", migratedSite)
	}
	changed, err = MigrateFallback(repository)
	if err != nil || changed {
		t.Fatalf("second migration = %v, %v; want no change", changed, err)
	}
}

func TestNormalizeCanonicalizesEnablesAndDeduplicatesRequiredChineseFallback(t *testing.T) {
	config := domain.LocalesConfig{
		SourceLocale: "en",
		Fallback:     []string{"en", DefaultFallback},
		Enabled: []domain.LocaleDefinition{
			{Code: "en", Label: "English", Enabled: false},
			{Code: "zh-cn", Label: "中文", Enabled: false},
			{Code: DefaultFallback, Label: "重复项", Enabled: true},
		},
	}
	if !Normalize(&config) {
		t.Fatal("expected legacy safety locale definitions to be normalized")
	}
	if config.SourceLocale != FixedSourceLocale || len(config.Enabled) != 2 || !sameStrings(config.Fallback, FixedFallbackOrder()) {
		t.Fatalf("normalized definitions = %#v", config.Enabled)
	}
	english := config.Enabled[0]
	if english.Code != "en" || english.Label != "English" || !english.Enabled || english.Status != domain.LocaleStatusProvisioning {
		t.Fatalf("ordinary English target was not permanently enabled = %#v", english)
	}
	chinese := config.Enabled[1]
	if chinese.Code != DefaultFallback || chinese.Label != "中文" || !chinese.Enabled || chinese.Status != domain.LocaleStatusReady {
		t.Fatalf("normalized Chinese fallback = %#v", chinese)
	}
	if Normalize(&config) {
		t.Fatal("normalized configuration should be idempotent")
	}
}

func TestNormalizePreservesTargetLifecycleStatus(t *testing.T) {
	config := domain.LocalesConfig{
		SourceLocale: FixedSourceLocale,
		Fallback:     FixedFallbackOrder(),
		Enabled: []domain.LocaleDefinition{
			{Code: FixedSourceLocale, Label: DefaultFallbackLabel, Enabled: true, Status: domain.LocaleStatusFailed},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
			{Code: "fr", Label: "Français", Enabled: true, Status: domain.LocaleStatusFailed},
			{Code: "de", Label: "Deutsch", Enabled: true, Status: domain.LocaleStatusBuilding},
		},
	}
	if !Normalize(&config) {
		t.Fatal("expected the fixed Chinese source status to be repaired")
	}
	if config.Enabled[0].Status != domain.LocaleStatusReady || config.Enabled[1].Status != domain.LocaleStatusProvisioning || config.Enabled[2].Status != domain.LocaleStatusFailed || config.Enabled[3].Status != domain.LocaleStatusBuilding {
		t.Fatalf("normalized lifecycle statuses = %#v", config.Enabled)
	}
	if Normalize(&config) {
		t.Fatal("normalized lifecycle statuses should be idempotent")
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

func TestMigrateFallbackMakesInterruptedBuildRetryable(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  FixedSourceLocale,
		Enabled: []domain.LocaleDefinition{
			{Code: FixedSourceLocale, Label: DefaultFallbackLabel, Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusBuilding},
		},
		Fallback: FixedFallbackOrder(),
	}
	if err := repository.WriteYAML("config/locales.yaml", config, false); err != nil {
		t.Fatal(err)
	}
	if changed, err := MigrateFallback(repository); err != nil || !changed {
		t.Fatalf("MigrateFallback() = %v, %v", changed, err)
	}
	if err := repository.ReadYAML("config/locales.yaml", &config); err != nil {
		t.Fatal(err)
	}
	if config.Enabled[1].Status != domain.LocaleStatusProvisioning {
		t.Fatalf("interrupted building status = %#v", config.Enabled[1])
	}
}
