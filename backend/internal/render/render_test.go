package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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

// captureServer records the last render payload and answers with a stub HTML,
// letting tests assert the Go→Node contract without a live worker.
func captureServer(t *testing.T) (*Service, func() map[string]any) {
	t.Helper()
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("render-cap-%d.sock", time.Now().UnixNano()))
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var captured map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"html":"<html/>"}`))
	})
	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = os.Remove(socket)
	})
	service := New("", socket, t.TempDir(), []string{})
	service.SetSiteOptions("https://blog.example.test", "zh-CN", map[model.Locale]string{
		"zh-CN": "zh-cn",
	})
	return service, func() map[string]any { return captured }
}

func TestSplitKindsSendDistinctPayloads(t *testing.T) {
	ctx := context.Background()
	home, captureHome := captureServer(t)
	if _, err := home.RenderHomePage(
		ctx, "zh-CN", "My Blog",
		[]HomeItem{{Title: "A", URL: "/zh-cn/posts/a/"}},
		[]HomeItem{{Title: "P", URL: "/zh-cn/posts/p/"}},
		&Pagination{Page: 1, Total: 1},
		"",
	); err != nil {
		t.Fatal(err)
	}
	if kind := captureHome()["kind"]; kind != "home" {
		t.Fatalf("home kind = %v", kind)
	}
	if captureHome()["pinned"] == nil {
		t.Fatal("home payload missing pinned")
	}

	category, captureCategory := captureServer(t)
	if _, err := category.RenderCategoryPage(
		ctx, "zh-CN", "Linux",
		CategoryProps{ID: "linux", Name: "Linux", Slug: "linux"},
		[]BreadcrumbItem{{Name: "Categories", URL: "/zh-cn/categories/"}},
		nil,
		[]HomeItem{{Title: "B", URL: "/zh-cn/posts/b/"}},
		"categories/linux",
	); err != nil {
		t.Fatal(err)
	}
	payload := captureCategory()
	if payload["kind"] != "category" || payload["category"] == nil {
		t.Fatalf("category payload = %#v", payload)
	}

	tag, captureTag := captureServer(t)
	if _, err := tag.RenderTagPage(
		ctx, "zh-CN", "Go",
		TagProps{ID: "go", Name: "Go", Slug: "go"},
		[]HomeItem{{Title: "C", URL: "/zh-cn/posts/c/"}},
		"tags/go",
	); err != nil {
		t.Fatal(err)
	}
	if kind := captureTag()["kind"]; kind != "tag" {
		t.Fatalf("tag kind = %v", kind)
	}

	archive, captureArchive := captureServer(t)
	if _, err := archive.RenderArchivePage(
		ctx, "zh-CN", "2026-08", "month", 2026, 8,
		[]ArchiveYear{{Year: 2026, Count: 1}},
		[]HomeItem{{Title: "D", URL: "/zh-cn/posts/d/"}},
		"archives/2026/08",
	); err != nil {
		t.Fatal(err)
	}
	payload = captureArchive()
	if payload["kind"] != "archive" || payload["scope"] != "month" {
		t.Fatalf("archive payload = %#v", payload)
	}

	links, captureLinks := captureServer(t)
	if _, err := links.RenderLinksPage(
		ctx, "zh-CN", "Links",
		[]LinkGroupItem{{ID: "friends", Name: "Friends", Links: []LinkItem{{Name: "X", URL: "https://x.test"}}}},
		"links",
	); err != nil {
		t.Fatal(err)
	}
	if kind := captureLinks()["kind"]; kind != "links" {
		t.Fatalf("links kind = %v", kind)
	}
}

// TestDeterministicRender renders the same article twice and asserts the
// output is byte-identical (docs/14 §4.3, the premise of `verify`).
func TestDeterministicRender(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rendererRoot := filepath.Join(wd, "..", "..", "..", "frontend", "renderer")
	rendererEntry := filepath.Join(rendererRoot, "src", "server.js")
	if _, err := os.Stat(rendererEntry); err != nil {
		t.Skip("renderer source not available")
	}
	// Source alone is not enough: the worker cannot start without its
	// dependencies, and that failure otherwise masquerades as a determinism bug.
	if _, err := os.Stat(filepath.Join(rendererRoot, "node_modules")); err != nil {
		t.Skip("renderer dependencies not installed (run: cd frontend/renderer && npm ci)")
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
