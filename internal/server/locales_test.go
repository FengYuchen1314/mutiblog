package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestSourceLocaleSwitchKeepsExistingContentOrigin(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"en", "zh-CN"}}
	site := domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}}, CreatedAt: now, UpdatedAt: now}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "existing-post", Title: "原文", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"en","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("switch response = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	unchanged, err := contentService.GetPost(post.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if locales.SourceLocale != "en" || site.SourceLocale != "en" || unchanged.Meta.SourceLocale != "zh-CN" || unchanged.Meta.Locales["zh-CN"].Origin != domain.LocaleOriginSource {
		t.Fatalf("source switch changed the wrong scope: locales=%q site=%q post=%#v", locales.SourceLocale, site.SourceLocale, unchanged.Meta)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"en","enabled":[{"code":"en","label":"English","enabled":true}]}`))
	recorder = httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"locale_in_use"`)) {
		t.Fatalf("disable original source response = %d %s", recorder.Code, recorder.Body.String())
	}
}
