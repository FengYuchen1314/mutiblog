package ai

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/index"
	"github.com/FengYuchen1314/mutiblog/internal/jobs"
	"github.com/FengYuchen1314/mutiblog/internal/model"
	"github.com/FengYuchen1314/mutiblog/internal/state"
)

func TestServiceWritesCompletedLocaleAndQueuesRender(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := content.NewStore(filepath.Join(root, "content"))
	article, err := store.CreateBundle(
		model.ContentPost,
		"zh-CN",
		model.FrontMatter{
			Title:        "标题",
			Slug:         "post",
			Description:  "摘要",
			Status:       model.StatusPublished,
			Author:       "admin",
			SourceLocale: "zh-CN",
		},
		"# 标题\n\n正文 `code`\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(index.Options{ContentRoot: store.Root()})
	ix.UpsertArticle(article)
	service := &Service{Provider: &fakeProvider{}, Store: store, Index: ix, Jobs: jobs.New(db), DB: db, Budget: 100}
	task, err := service.Enqueue(context.Background(), article.ID, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(jobPayload{TaskID: task.ID, ArticleID: string(article.ID), Target: "en"})
	if err = service.Run(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	updated, ok := ix.Article(article.ID)
	if !ok || updated.Trans["en"].Status != model.TSCompleted {
		t.Fatalf("translation state = %#v", updated.Trans["en"])
	}
	version := updated.Versions["en"]
	if version == nil || !strings.Contains(version.Body, "`code`") || !strings.Contains(version.Body, "译正文") {
		t.Fatalf("version = %#v", version)
	}
	if version.Front.Title != "译标题" || version.Front.Description != "译摘要" {
		t.Fatalf("front = %#v", version.Front)
	}
	job, err := service.Jobs.Claim(context.Background(), "render", "test")
	if err != nil || job == nil {
		t.Fatalf("render job = %#v err=%v", job, err)
	}
	tasks, err := service.List(context.Background(), 10)
	if err != nil || len(tasks) != 1 || tasks[0].Status != "completed" {
		t.Fatalf("tasks=%#v err=%v", tasks, err)
	}
}
