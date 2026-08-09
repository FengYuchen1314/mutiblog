package render

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestDeterministicRender renders the same article twice and asserts the
// output is byte-identical (docs/14 §4.3, the premise of `verify`).
func TestDeterministicRender(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rendererEntry := filepath.Join(wd, "..", "..", "..", "frontend", "renderer", "src", "server.js")
	if _, err := os.Stat(rendererEntry); err != nil {
		t.Skip("renderer source not available")
	}
	output := t.TempDir()
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("render-det-%d.sock", time.Now().UnixNano()))
	service := New("", socket, output, []string{"node", rendererEntry})
	service.SetMarkdownOptions(true, true, true, false)
	service.SetSiteOptions("https://blog.example.test", "zh-CN", map[model.Locale]string{
		"zh-CN": "zh-cn",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	article := &model.Article{
		ID:   model.ArticleID("deterministic-test"),
		Type: model.ContentPost,
		Versions: map[model.Locale]*model.ArticleVersion{
			"zh-CN": {
				Front: model.FrontMatter{
					Title:  "确定性渲染",
					Slug:   "deterministic",
					Status: model.StatusPublished,
				},
				Body: "# 标题\n\n正文内容。\n\n```go\npackage main\n```\n",
			},
		},
	}
	first, err := service.Render(ctx, article, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Render(ctx, article, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	firstData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstData, secondData) {
		t.Fatal("same article rendered differently across runs")
	}
}
