package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type localeBuildPublisher struct {
	request publisher.BuildRequest
	err     error
}

func (p *localeBuildPublisher) Build(ctx context.Context) (publisher.BuildReport, error) {
	p.request = publisher.BuildRequestFromContext(ctx)
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}, p.err
}

func TestSourceLocaleSwitchKeepsExistingContentOrigin(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"zh-CN"}}
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
	builds := &countingSitePublisher{}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"en","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-MutiBlog-Static-Build") != "succeeded" {
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
	if locales.SourceLocale != "en" || len(locales.Fallback) != 1 || locales.Fallback[0] != "zh-CN" || site.SourceLocale != "en" || site.Locales["en"].Title != "测试站" || unchanged.Meta.SourceLocale != "zh-CN" || unchanged.Meta.Locales["zh-CN"].Origin != domain.LocaleOriginSource {
		t.Fatalf("source switch changed the wrong scope: locales=%q site=%q post=%#v", locales.SourceLocale, site.SourceLocale, unchanged.Meta)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"en","enabled":[{"code":"en","label":"English","enabled":true}]}`))
	recorder = httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("remove Chinese fallback response = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		t.Fatal(err)
	}
	if len(locales.Enabled) != 2 || !definitionsEnabled(locales.Enabled, "zh-CN") {
		t.Fatalf("missing fallback was not restored: %#v", locales.Enabled)
	}
	var fallback domain.LocaleDefinition
	for _, definition := range locales.Enabled {
		if definition.Code == "zh-CN" {
			fallback = definition
		}
	}
	if fallback.Label != "简体中文" {
		t.Fatalf("restored fallback label = %q", fallback.Label)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"en","enabled":[{"code":"en","label":"English","enabled":true},{"code":"zh-CN","label":"中文安全回退","enabled":false}]}`))
	recorder = httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable Chinese fallback response = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		t.Fatal(err)
	}
	if !definitionsEnabled(locales.Enabled, "zh-CN") {
		t.Fatalf("disabled fallback remained unavailable: %#v", locales.Enabled)
	}
	for _, definition := range locales.Enabled {
		if definition.Code == "zh-CN" && (!definition.Enabled || definition.Label != "中文安全回退") {
			t.Fatalf("disabled fallback was not normalized: %#v", definition)
		}
	}
	if builds.builds.Load() != 3 {
		t.Fatalf("locale updates triggered %d builds, want 3", builds.builds.Load())
	}
}

func TestSourceLocaleSwitchSeedsMissingSiteCopyFromPreviousSource(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "fr",
		Enabled: []domain.LocaleDefinition{
			{Code: "fr", Label: "Français", Enabled: true},
			{Code: "ja", Label: "日本語", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}
	site := domain.SiteConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "fr", Locales: map[string]domain.LocalizedSite{"fr": {Title: "Site français", Description: "Description"}}, CreatedAt: now, UpdatedAt: now}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: &countingSitePublisher{}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"ja","enabled":[{"code":"fr","label":"Français","enabled":true},{"code":"ja","label":"日本語","enabled":true},{"code":"en","label":"English","enabled":true},{"code":"zh-CN","label":"简体中文","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("source switch response = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	if site.SourceLocale != "ja" || site.Locales["ja"].Title != "Site français" || site.Locales["ja"].Description != "Description" || site.Locales["fr"].Title != "Site français" {
		t.Fatalf("seeded site copy = %#v", site)
	}
}

func TestUpdateLocalesAlwaysRestoresRequiredChineseFallback(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "fr",
		Enabled: []domain.LocaleDefinition{
			{Code: "fr", Label: "Français", Enabled: true},
			{Code: "en", Label: "English", Enabled: true},
			{Code: "zh-CN", Label: "简体中文", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	}
	site := domain.SiteConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "fr", Locales: map[string]domain.LocalizedSite{"fr": {Title: "Site"}}, CreatedAt: now, UpdatedAt: now}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	builds := &countingSitePublisher{}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds}

	for _, body := range []string{
		`{"sourceLocale":"fr","enabled":[{"code":"fr","label":"Français","enabled":true}]}`,
		`{"sourceLocale":"fr","enabled":[{"code":"fr","label":"Français","enabled":true},{"code":"en","label":"English fallback","enabled":false},{"code":"zh-CN","label":"中文回退","enabled":false}]}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(body))
		recorder := httptest.NewRecorder()
		server.handleUpdateLocales(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("required fallback normalization response = %d %s", recorder.Code, recorder.Body.String())
		}
		if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
			t.Fatal(err)
		}
		if len(locales.Fallback) != 1 || locales.Fallback[0] != "zh-CN" || definitionsEnabled(locales.Enabled, "en") || !definitionsEnabled(locales.Enabled, "zh-CN") {
			t.Fatalf("required Chinese fallback was not restored: %#v", locales)
		}
	}
	if builds.builds.Load() != 2 {
		t.Fatalf("locale updates triggered %d builds, want 2", builds.builds.Load())
	}
}

func TestUpdateLocalesReturnsSavedConfigWhenSameRequestBuildFails(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	locales := domain.LocalesConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}, {Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"zh-CN"}}
	site := domain.SiteConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}}, CreatedAt: now, UpdatedAt: now}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	builds := &localeBuildPublisher{err: errors.New("renderer unavailable")}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds}
	handler := server.withBuildTaskRequest(http.HandlerFunc(server.handleUpdateLocales))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"en","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
	request.Header.Set("X-MutiBlog-Task-ID", "locale-build-123")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted || recorder.Header().Get("X-MutiBlog-Static-Build") != "failed" {
		t.Fatalf("failed build response = %d %q %s", recorder.Code, recorder.Header().Get("X-MutiBlog-Static-Build"), recorder.Body.String())
	}
	var response struct {
		Locales domain.LocalesConfig `json:"locales"`
		Build   struct {
			Status string `json:"status"`
		} `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Locales.SourceLocale != "en" || response.Build.Status != "failed" {
		t.Fatalf("failed build response body = %#v", response)
	}
	if builds.request.TaskID != "locale-build-123" || builds.request.Operation != "localization-rebuild" {
		t.Fatalf("build request = %#v", builds.request)
	}
	if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		t.Fatal(err)
	}
	if locales.SourceLocale != "en" {
		t.Fatalf("saved locale source = %q", locales.SourceLocale)
	}
}
