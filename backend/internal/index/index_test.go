package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
)

func TestCategoryInheritanceAndLazyBody(t *testing.T) {
	root := t.TempDir()
	contentRoot, dataRoot := filepath.Join(root, "content"), filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataRoot, "categories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "categories", "technology.yaml"), []byte("id: technology\norder: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "categories", "linux.yaml"), []byte("id: linux\nparent: technology\norder: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := content.NewStore(contentRoot)
	front := model.FrontMatter{Title: "Post", Slug: "post", Status: model.StatusPublished, Author: "admin", SourceLocale: "en", Categories: []string{"linux"}, Date: time.Now()}
	article, err := store.CreateBundle(model.ContentPost, "en", front, "long body")
	if err != nil {
		t.Fatal(err)
	}
	ix := New(Options{ContentRoot: contentRoot, BodyResidentLimitBytes: 1})
	if err := ix.RebuildAll(context.Background(), store, taxonomy.NewStore(dataRoot)); err != nil {
		t.Fatal(err)
	}
	items, total := ix.List(ListQuery{Type: model.ContentPost, Locale: "en", Category: "technology"})
	if total != 1 || items[0].ID != article.ID {
		t.Fatalf("items=%v total=%d", items, total)
	}
	if ix.Stats().BodyMode != BodyLazy {
		t.Fatalf("mode=%v", ix.Stats().BodyMode)
	}
	body, err := ix.Body(article.ID, "en")
	if err != nil || body != "long body\n" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestPublishedPostsUsesStableIDTieBreaker(t *testing.T) {
	ix := New(Options{})
	date := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	makeArticle := func(id model.ArticleID) *model.Article {
		return &model.Article{ID: id, Type: model.ContentPost, Source: "en", Versions: map[model.Locale]*model.ArticleVersion{
			"en": {Locale: "en", Front: model.FrontMatter{ID: id, Title: string(id), Slug: string(id), Status: model.StatusPublished, SourceLocale: "en", Locale: "en", Date: date}},
		}}
	}
	ix.UpsertArticle(makeArticle("z-last"))
	ix.UpsertArticle(makeArticle("a-first"))
	posts := ix.PublishedPosts("en")
	if len(posts) != 2 || posts[0].ID != "a-first" || posts[1].ID != "z-last" {
		t.Fatalf("stable order = %#v", posts)
	}
}
