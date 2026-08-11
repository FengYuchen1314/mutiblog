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
	server.writePublishedResource(success, request, http.StatusOK, resource, "Category", "example")
	if success.Code != http.StatusOK || success.Header().Get("X-MutiBlog-Static-Build") != "succeeded" || !strings.Contains(success.Body.String(), `"id":"example"`) {
		t.Fatalf("successful response = %d %q %s", success.Code, success.Header().Get("X-MutiBlog-Static-Build"), success.Body.String())
	}

	failed := httptest.NewRecorder()
	server.publisher = failedResourcePublisher{}
	server.writePublishedResource(failed, request, http.StatusOK, resource, "Category", "example")
	if failed.Code != http.StatusAccepted || failed.Header().Get("X-MutiBlog-Static-Build") != "failed" || !strings.Contains(failed.Body.String(), `"revision":2`) {
		t.Fatalf("failed response = %d %q %s", failed.Code, failed.Header().Get("X-MutiBlog-Static-Build"), failed.Body.String())
	}
}
