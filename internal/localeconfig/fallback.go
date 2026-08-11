package localeconfig

import (
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"golang.org/x/text/language"
)

const (
	DefaultFallback      = "zh-CN"
	DefaultFallbackLabel = "简体中文"
)

// FixedFallbackOrder returns the confirmed Chinese content fallback after the
// requested locale and before an entity's immutable source locale.
func FixedFallbackOrder() []string {
	return []string{DefaultFallback}
}

// Normalize applies the fixed Chinese content fallback policy while preserving
// every unrelated locale definition. Simplified Chinese is always canonical,
// enabled, and unique; an existing English target remains an ordinary,
// administrator-managed locale. It reports whether the configuration changed.
func Normalize(config *domain.LocalesConfig) bool {
	changed := false
	expectedFallback := FixedFallbackOrder()
	if !sameStrings(config.Fallback, expectedFallback) {
		config.Fallback = expectedFallback
		changed = true
	}

	definitions := make([]domain.LocaleDefinition, 0, len(config.Enabled)+len(expectedFallback))
	found := make(map[string]bool, len(expectedFallback))
	for _, definition := range config.Enabled {
		fallbackCode := canonicalFallback(definition.Code)
		if fallbackCode == "" {
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
		if !definition.Enabled {
			definition.Enabled = true
			changed = true
		}
		definitions = append(definitions, definition)
	}
	for _, fallbackCode := range expectedFallback {
		if !found[fallbackCode] {
			definitions = append(definitions, domain.LocaleDefinition{Code: fallbackCode, Label: fallbackLabel(fallbackCode), Enabled: true})
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

// MigrateFallback safely rewrites an existing locale configuration so the
// Chinese fallback order and its enabled definition satisfy Normalize. A
// missing file belongs to an uninitialized repository and is intentionally a
// no-op.
func MigrateFallback(repository *fsrepo.Repository) (bool, error) {
	exists, err := repository.Exists("config/locales.yaml")
	if err != nil || !exists {
		return false, err
	}
	var config domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return false, err
	}
	if !Normalize(&config) {
		return false, nil
	}
	if err := repository.WriteYAML("config/locales.yaml", config, false); err != nil {
		return false, err
	}
	return true, nil
}
