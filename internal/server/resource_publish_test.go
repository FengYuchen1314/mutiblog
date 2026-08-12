package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type failedResourcePublisher struct{}

func (failedResourcePublisher) Build(context.Context) (publisher.BuildReport, error) {
	return publisher.BuildReport{}, errors.New("build failed")
}

func TestPublishedResourceReportsBuildOutcomeWithoutChangingShape(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/taxonomies/categories/example/locales/en", nil)
	resource := map[string]any{"id": "example", "revision": 2}

	success := httptest.NewRecorder()
	server := &Server{publisher: fakeSitePublisher{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	server.writePublishedResource(success, request, http.StatusOK, resource, "Category", "example", false)
	if success.Code != http.StatusOK || success.Header().Get("X-MutiBlog-Static-Build") != "succeeded" || !strings.Contains(success.Body.String(), `"id":"example"`) {
		t.Fatalf("successful response = %d %q %s", success.Code, success.Header().Get("X-MutiBlog-Static-Build"), success.Body.String())
	}

	failed := httptest.NewRecorder()
	server.publisher = failedResourcePublisher{}
	server.writePublishedResource(failed, request, http.StatusOK, resource, "Category", "example", false)
	if failed.Code != http.StatusAccepted || failed.Header().Get("X-MutiBlog-Static-Build") != "failed" || !strings.Contains(failed.Body.String(), `"revision":2`) {
		t.Fatalf("failed response = %d %q %s", failed.Code, failed.Header().Get("X-MutiBlog-Static-Build"), failed.Body.String())
	}
}

func TestPublishedResourceQueuesVisibleLocaleRefreshWithoutChangingLocaleStatus(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Label: "English", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "de", Label: "Deutsch", Enabled: true}, // legacy public/ready
			{Code: "fr", Label: "Français", Enabled: true, Status: domain.LocaleStatusFailed},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	starter := &recordingLocaleTaskStarter{}
	server := &Server{
		repository:        repository,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher:         builds,
		localeTaskManager: starter,
	}
	resource := map[string]any{"id": "example", "revision": 1}
	recorder := httptest.NewRecorder()
	server.writePublishedResource(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/links/items", nil), http.StatusCreated, resource, "Link", "example", true)

	if recorder.Code != http.StatusAccepted || recorder.Header().Get("X-MutiBlog-Static-Build") != "deferred" || recorder.Header().Get("X-MutiBlog-Localization-Task") == "" {
		t.Fatalf("refresh response = %d headers:%#v body:%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if len(starter.starts) != 1 || !starter.starts[0].PreserveLocaleVisibility || !equalStrings(starter.starts[0].Locales, []string{"de", "en"}) {
		t.Fatalf("refresh start = %#v", starter.starts)
	}
	if builds.builds.Load() != 0 {
		t.Fatalf("deferred resource triggered %d direct builds", builds.builds.Load())
	}
	for _, definition := range readLocaleTestConfig(t, repository).Enabled {
		switch definition.Code {
		case "en":
			if definition.Status != domain.LocaleStatusReady {
				t.Fatalf("ready locale changed = %#v", definition)
			}
		case "fr":
			if definition.Status != domain.LocaleStatusFailed {
				t.Fatalf("failed locale changed = %#v", definition)
			}
		case "ja":
			if definition.Status != domain.LocaleStatusProvisioning {
				t.Fatalf("provisioning locale changed = %#v", definition)
			}
		}
	}
}

func TestPublishedResourceBuildsSynchronouslyForSourceOnlySite(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	starter := &recordingLocaleTaskStarter{}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds, localeTaskManager: starter}
	recorder := httptest.NewRecorder()
	server.writePublishedResource(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/links/items", nil), http.StatusCreated, map[string]any{"id": "example"}, "Link", "example", true)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("X-MutiBlog-Static-Build") != "succeeded" || builds.builds.Load() != 1 || len(starter.starts) != 0 {
		t.Fatalf("source-only response = %d header:%q builds:%d starts:%#v", recorder.Code, recorder.Header().Get("X-MutiBlog-Static-Build"), builds.builds.Load(), starter.starts)
	}
}
