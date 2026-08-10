package i18n

import (
	"github.com/FengYuchen1314/mutiblog/internal/config"
	"testing"
)

func TestRegistryCanonicalAndPrefixes(t *testing.T) {
	r, err := New(
		config.I18nConfig{
			DefaultLocale: "zh-CN",
			SourceLocale:  "zh-CN",
			Locales: []config.LocaleConfig{
				{Code: "zh-CN", URLPrefix: "zh-cn", Enabled: true},
				{Code: "zh-TW", URLPrefix: "zh-tw", Enabled: true},
				{Code: "en", URLPrefix: "en", Enabled: true},
			},
			LocaleAliases: map[string]string{"zh-HK": "zh-TW"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.URLPrefix("zh-CN"); got != "zh-cn" {
		t.Fatal(got)
	}
	if got, ok := r.FromPrefix("ZH-TW"); !ok || got != "zh-TW" {
		t.Fatalf("%q %v", got, ok)
	}
	if got, ok := r.Alias("zh-HK"); !ok || got != "zh-TW" {
		t.Fatalf("%q %v", got, ok)
	}
}
