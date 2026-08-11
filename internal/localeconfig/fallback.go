package localeconfig

import (
	"errors"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"golang.org/x/text/language"
)

const (
	FixedSourceLocale    = "zh-CN"
	DefaultFallback      = FixedSourceLocale
	DefaultFallbackLabel = "简体中文"
)

// FixedFallbackOrder returns the confirmed Chinese content fallback after the
// requested locale and before an entity's immutable source locale.
func FixedFallbackOrder() []string {
	return []string{DefaultFallback}
}

// Normalize applies the fixed Chinese source and fallback policy while
// preserving every previously added locale. Added locales remain permanently
// enabled and Simplified Chinese is always canonical, unique, and ready.
// Historical enabled targets without a lifecycle status remain statusless so
// their last-known-good fallback release stays available until the next locale
// save provisions an exact translation. A historically disabled target is
// retained but hidden in provisioning state instead of being republished
// without a completeness check. It reports whether the configuration changed.
func Normalize(config *domain.LocalesConfig) bool {
	changed := false
	if config.SourceLocale != FixedSourceLocale {
		config.SourceLocale = FixedSourceLocale
		changed = true
	}
	expectedFallback := FixedFallbackOrder()
	if !sameStrings(config.Fallback, expectedFallback) {
		config.Fallback = expectedFallback
		changed = true
	}

	definitions := make([]domain.LocaleDefinition, 0, len(config.Enabled)+len(expectedFallback))
	found := make(map[string]bool, len(expectedFallback))
	for _, definition := range config.Enabled {
		wasEnabled := definition.Enabled
		if !definition.Enabled {
			definition.Enabled = true
			changed = true
		}
		fallbackCode := canonicalFallback(definition.Code)
		if fallbackCode == "" {
			switch strings.TrimSpace(definition.Status) {
			case "", domain.LocaleStatusProvisioning, domain.LocaleStatusBuilding, domain.LocaleStatusReady, domain.LocaleStatusFailed:
				// Empty is the deliberate legacy compatibility state.
			default:
				definition.Status = domain.LocaleStatusFailed
				changed = true
			}
			if !wasEnabled && definition.Status != domain.LocaleStatusFailed {
				definition.Status = domain.LocaleStatusProvisioning
				changed = true
			}
			definitions = append(definitions, definition)
			continue
		}
		if found[fallbackCode] {
			changed = true
			continue
		}
		found[fallbackCode] = true
		if definition.Code != fallbackCode {
			definition.Code = fallbackCode
			changed = true
		}
		if strings.TrimSpace(definition.Label) == "" {
			definition.Label = fallbackLabel(fallbackCode)
			changed = true
		}
		if definition.Status != domain.LocaleStatusReady {
			definition.Status = domain.LocaleStatusReady
			changed = true
		}
		definitions = append(definitions, definition)
	}
	for _, fallbackCode := range expectedFallback {
		if !found[fallbackCode] {
			definitions = append(definitions, domain.LocaleDefinition{Code: fallbackCode, Label: fallbackLabel(fallbackCode), Enabled: true, Status: domain.LocaleStatusReady})
			changed = true
		}
	}
	if changed {
		config.Enabled = definitions
	}
	return changed
}

func canonicalFallback(raw string) string {
	tag, err := language.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	canonical := tag.String()
	if canonical == DefaultFallback {
		return canonical
	}
	return ""
}

func fallbackLabel(_ string) string {
	return DefaultFallbackLabel
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// MigrateFallback safely rewrites an existing locale configuration and its
// site-owned source copy so the fixed Chinese source and fallback invariants
// satisfy Normalize. A missing locale file belongs to an uninitialized
// repository and is intentionally a no-op.
func MigrateFallback(repository *fsrepo.Repository) (bool, error) {
	exists, err := repository.Exists("config/locales.yaml")
	if err != nil || !exists {
		return false, err
	}
	var config domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return false, err
	}
	previousSource := config.SourceLocale
	configChanged := Normalize(&config)
	// Building is a process-local two-phase handoff between complete durable
	// translations and one atomic static release. A restart cannot know whether
	// that release committed, so make the target safely retryable and keep it
	// hidden from public APIs.
	for index := range config.Enabled {
		if config.Enabled[index].Code != FixedSourceLocale && config.Enabled[index].Status == domain.LocaleStatusBuilding {
			config.Enabled[index].Status = domain.LocaleStatusProvisioning
			configChanged = true
		}
	}
	siteExists, err := repository.Exists("config/site.yaml")
	if err != nil {
		return false, err
	}
	var previousSite, site domain.SiteConfig
	siteChanged := false
	if siteExists {
		if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
			return false, err
		}
		previousSite = site
		if site.Locales != nil {
			cloned := make(map[string]domain.LocalizedSite, len(site.Locales)+1)
			for locale, localized := range site.Locales {
				cloned[locale] = localized
			}
			site.Locales = cloned
		}
		if site.Locales == nil {
			site.Locales = make(map[string]domain.LocalizedSite)
		}
		if _, exists := site.Locales[FixedSourceLocale]; !exists {
			copy, available := site.Locales[site.SourceLocale]
			if !available {
				copy, available = site.Locales[previousSource]
			}
			if !available || strings.TrimSpace(copy.Title) == "" {
				return false, errors.New("site source copy is unavailable for fixed Chinese-source migration")
			}
			site.Locales[FixedSourceLocale] = copy
			siteChanged = true
		}
		if site.SourceLocale != FixedSourceLocale {
			site.SourceLocale = FixedSourceLocale
			siteChanged = true
		}
		if siteChanged {
			site.UpdatedAt = time.Now().UTC()
		}
	}
	if !configChanged && !siteChanged {
		return false, nil
	}
	if siteChanged {
		if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
			return false, err
		}
	}
	if configChanged {
		if err := repository.WriteYAML("config/locales.yaml", config, false); err != nil {
			if siteChanged {
				if rollbackErr := repository.WriteYAML("config/site.yaml", previousSite, false); rollbackErr != nil {
					return false, errors.Join(err, rollbackErr)
				}
			}
			return false, err
		}
	}
	return true, nil
}
