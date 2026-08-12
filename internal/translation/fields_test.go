package translation

import (
	"errors"
	"testing"
)

func TestCanonicalLocalePairRejectsInvalidAndSameLocale(t *testing.T) {
	for _, pair := range [][2]string{{"", "ja"}, {"zh-CN", ""}, {"invalid_locale", "ja"}, {"zh-CN", "zh-cn"}} {
		if _, _, err := canonicalLocalePair(pair[0], pair[1]); !errors.Is(err, ErrInvalidFields) {
			t.Fatalf("canonicalLocalePair(%q, %q) error = %v", pair[0], pair[1], err)
		}
	}
	source, target, err := canonicalLocalePair("zh-cn", "JA")
	if err != nil || source != "zh-CN" || target != "ja" {
		t.Fatalf("canonical locale pair = %q, %q, %v", source, target, err)
	}
}

func TestValidateTranslationFieldsAndExactTranslatedSet(t *testing.T) {
	fields := map[string]string{"title": "站点", "footer.slogan": "", "menu-primary": "主菜单"}
	keys, _, err := validateTranslationFields(fields)
	if err != nil || len(keys) != 3 || keys[0] != "footer.slogan" {
		t.Fatalf("validated fields = %#v, %v", keys, err)
	}
	translated := map[string]string{"title": "Site", "footer.slogan": "", "menu-primary": "Primary menu"}
	if err := validateTranslatedFieldSet(fields, translated, keys); err != nil {
		t.Fatal(err)
	}
	delete(translated, "title")
	if !errors.Is(validateTranslatedFieldSet(fields, translated, keys), ErrUnsafeOutput) {
		t.Fatal("missing translated key was accepted")
	}
	if _, _, err := validateTranslationFields(map[string]string{"../title": "站点"}); !errors.Is(err, ErrInvalidFields) {
		t.Fatalf("unsafe key error = %v", err)
	}
}
