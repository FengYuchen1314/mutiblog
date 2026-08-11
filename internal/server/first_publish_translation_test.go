package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type translationReceiptPublisher struct {
	translator *translation.Service
	sawReceipt bool
	err        error
}

func (sitePublisher *translationReceiptPublisher) Build(context.Context) (publisher.BuildReport, error) {
	tasks, err := sitePublisher.translator.List()
	if err != nil {
		sitePublisher.err = err
	} else {
		for _, task := range tasks {
			if task.Status == "failed" && task.Error == "provider-unavailable" {
				sitePublisher.sawReceipt = true
			}
		}
	}
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
}

func TestImmediateFirstPublishRecordsProviderPreflightFailureBeforeSourceBuild(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN",
				Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"zh-CN"},
			}, false); err != nil {
				t.Fatal(err)
			}
			contentService := content.NewService(repository)
			var item domain.Post
			if kind == "Page" {
				item, err = contentService.CreatePage(content.CreatePageInput{ID: "preflight-page", Title: "private page title", Markdown: "private page body"})
			} else {
				item, err = contentService.CreatePost(content.CreatePostInput{ID: "preflight-post", Title: "private post title", Markdown: "private post body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			translator := translation.NewService(repository, contentService, ai.NewService(repository, ai.Client{}), nil)
			defer translator.Close()
			builds := &translationReceiptPublisher{translator: translator}
			backups := backup.NewService(repository)
			defer backups.Close()
			app := &Server{
				repository: repository, content: contentService, translator: translator, backups: backups, publisher: builds,
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			body, err := json.Marshal(map[string]int{"revision": item.Meta.Revision})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			request.SetPathValue("id", item.Meta.ID)
			recorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handlePublishPage(recorder, request)
			} else {
				app.handlePublishPost(recorder, request)
			}
			var response struct {
				Build struct {
					Status string `json:"status"`
				} `json:"build"`
				Translation struct {
					Status string `json:"status"`
					TaskID string `json:"taskId"`
				} `json:"translation"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusOK || response.Build.Status != "succeeded" || response.Translation.Status != "not-configured" || response.Translation.TaskID == "" {
				t.Fatalf("first-publish response = status %d, %#v, body %s", recorder.Code, response, recorder.Body.String())
			}
			if builds.err != nil || !builds.sawReceipt {
				t.Fatalf("source build did not observe durable translation receipt: saw=%t err=%v", builds.sawReceipt, builds.err)
			}
			stored, err := translator.Get(response.Translation.TaskID)
			if err != nil || stored.Status != "failed" || stored.Error != "provider-unavailable" || stored.EntityKind != kind || stored.EntityID != item.Meta.ID || len(stored.Targets) != 1 || stored.SourceContent == "" || stored.Targets[0].ExpectedContent == "" {
				t.Fatalf("durable first-publish preflight task = %#v, %v", stored, err)
			}
			items, err := app.listAdminTasks()
			if err != nil || len(items) != 1 || items[0].ID != stored.ID || items[0].Status != "failed" || items[0].Error != "provider-unavailable" {
				t.Fatalf("admin task list = %#v, %v", items, err)
			}
			getRequest := httptest.NewRequest(http.MethodGet, "/", nil)
			getRequest.SetPathValue("id", stored.ID)
			getRecorder := httptest.NewRecorder()
			app.handleGetTask(getRecorder, getRequest)
			var got adminTask
			if err := json.Unmarshal(getRecorder.Body.Bytes(), &got); err != nil || getRecorder.Code != http.StatusOK || got.ID != stored.ID || got.Status != "failed" || got.Error != "provider-unavailable" {
				t.Fatalf("admin task get = status %d, %#v, %v, body %s", getRecorder.Code, got, err, getRecorder.Body.String())
			}
			payload, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), "private ") {
				t.Fatalf("durable preflight task exposed source content: %s", payload)
			}
		})
	}
}
