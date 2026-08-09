package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fengyuchen/mutiblog/internal/model"
)

func TestCollectionAlternatesUseEnabledLocaleSet(t *testing.T) {
	service := New("", "", "", nil)
	service.SetSiteOptions("https://blog.example.test/", "zh-CN", map[model.Locale]string{
		"zh-CN": "zh-cn",
		"en":    "en",
	})
	got := service.collectionAlternates("categories/linux")
	want := []map[string]string{
		{"hrefLang": "en", "href": "https://blog.example.test/en/categories/linux/"},
		{"hrefLang": "zh-CN", "href": "https://blog.example.test/zh-cn/categories/linux/"},
		{"hrefLang": "x-default", "href": "https://blog.example.test/zh-cn/categories/linux/"},
	}
	if len(got) != len(want) {
		t.Fatalf("alternates=%#v", got)
	}
	for i := range want {
		if got[i]["hrefLang"] != want[i]["hrefLang"] || got[i]["href"] != want[i]["href"] {
			t.Fatalf("alternate %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestRemoveDeletesOnlyTheContentUnit(t *testing.T) {
	output := t.TempDir()
	service := New("", "", output, nil)
	service.SetSiteOptions("https://blog.example.test", "en", map[model.Locale]string{"en": "en"})
	article := &model.Article{Type: model.ContentPost, Versions: map[model.Locale]*model.ArticleVersion{
		"en": {Front: model.FrontMatter{Slug: "remove-me"}},
	}}
	target := filepath.Join(output, "en", "posts", "remove-me", "index.html")
	neighbor := filepath.Join(output, "en", "posts", "keep-me", "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(neighbor), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(neighbor, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.Remove(article, "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("removed page still exists: %v", err)
	}
	if got, err := os.ReadFile(neighbor); err != nil || string(got) != "keep" {
		t.Fatalf("neighbor was changed: %q, %v", got, err)
	}
}
