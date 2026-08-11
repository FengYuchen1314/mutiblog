package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestPostAndPageLocaleHandlersRejectAIManagedTargetsBeforeDecodingBody(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion,
				SourceLocale:  "zh-CN",
				Enabled: []domain.LocaleDefinition{
					{Code: "zh-CN", Label: "简体中文", Enabled: true},
					{Code: "en", Label: "English", Enabled: true},
				},
				Fallback: []string{"zh-CN"},
			}, false); err != nil {
				t.Fatal(err)
			}
			contentService := content.NewService(repository)
			var item domain.Post
			if kind == "Page" {
				item, err = contentService.CreatePage(content.CreatePageInput{ID: "locale-lock-page", Title: "源页面", Markdown: "正文"})
			} else {
				item, err = contentService.CreatePost(content.CreatePostInput{ID: "locale-lock-post", Title: "源文章", Markdown: "正文"})
			}
			if err != nil {
				t.Fatal(err)
			}
			app := &Server{content: contentService, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

			blockedRequest := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString("{"))
			blockedRequest.SetPathValue("id", item.Meta.ID)
			blockedRequest.SetPathValue("locale", "en")
			blockedRecorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handleUpdatePageLocale(blockedRecorder, blockedRequest)
			} else {
				app.handleUpdatePostLocale(blockedRecorder, blockedRequest)
			}
			var blocked struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(blockedRecorder.Body.Bytes(), &blocked); err != nil {
				t.Fatal(err)
			}
			if blockedRecorder.Code != http.StatusForbidden || blocked.Code != "content_locale_ai_managed" {
				t.Fatalf("blocked response = %d, %#v, body %s", blockedRecorder.Code, blocked, blockedRecorder.Body.String())
			}
			unchanged, err := contentForLocaleAccessTest(contentService, kind, item.Meta.ID)
			if err != nil || unchanged.Meta.Revision != item.Meta.Revision {
				t.Fatalf("blocked target changed content = %#v, %v", unchanged, err)
			}
			if _, exists := unchanged.Content["en"]; exists {
				t.Fatalf("blocked target content was created: %#v", unchanged.Content)
			}

			body, err := json.Marshal(updatePostLocaleRequest{Revision: item.Meta.Revision, Title: "更新后的源内容", Markdown: "新正文"})
			if err != nil {
				t.Fatal(err)
			}
			sourceRequest := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
			sourceRequest.SetPathValue("id", item.Meta.ID)
			sourceRequest.SetPathValue("locale", "zh-cn")
			sourceRecorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handleUpdatePageLocale(sourceRecorder, sourceRequest)
			} else {
				app.handleUpdatePostLocale(sourceRecorder, sourceRequest)
			}
			if sourceRecorder.Code != http.StatusOK {
				t.Fatalf("source update response = %d, body %s", sourceRecorder.Code, sourceRecorder.Body.String())
			}
			updated, err := contentForLocaleAccessTest(contentService, kind, item.Meta.ID)
			if err != nil || updated.Content["zh-CN"].Title != "更新后的源内容" || updated.Meta.Revision != item.Meta.Revision+1 {
				t.Fatalf("source update = %#v, %v", updated, err)
			}
		})
	}
}

func contentForLocaleAccessTest(service *content.Service, kind, id string) (domain.Post, error) {
	if kind == "Page" {
		return service.GetPage(id)
	}
	return service.GetPost(id)
}
