package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

const manualIndexTaskID = "index-rebuild-20260811T010203.000000000Z-aabbccdd"

func TestManualIndexRebuildCreatesQueryableDurableTask(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	setup := map[string]string{
		"siteTitle": "Index task", "baseUrl": server.URL, "sourceLocale": "zh-CN", "adminLocale": "zh-CN",
		"timezone": "Asia/Shanghai", "username": "admin", "password": "correct horse battery staple",
	}
	response := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var setupResult struct {
		Session struct {
			CSRFToken string `json:"csrfToken"`
		} `json:"session"`
	}
	if err := decodeIndexTaskResponse(response, &setupResult); err != nil {
		t.Fatal(err)
	}

	response = requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/admin/index/rebuild", nil, map[string]string{
		"X-CSRF-Token": setupResult.Session.CSRFToken, "X-MutiBlog-Index-Task-ID": manualIndexTaskID,
	})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("index rebuild status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var queued struct {
		Task adminTask `json:"task"`
	}
	if err := decodeIndexTaskResponse(response, &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Task.ID != manualIndexTaskID || queued.Task.Kind != "IndexRebuild" || queued.Task.Operation != "rebuild-search-index" {
		t.Fatalf("queued index task = %#v", queued.Task)
	}

	response, err = client.Get(server.URL + "/api/v1/admin/tasks/" + manualIndexTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("index task lookup = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var stored adminTask
	if err := decodeIndexTaskResponse(response, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != manualIndexTaskID || stored.Kind != "IndexRebuild" || stored.Progress.Total != 3 {
		t.Fatalf("stored index task = %#v", stored)
	}
}

func decodeIndexTaskResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	return json.NewDecoder(response.Body).Decode(destination)
}
