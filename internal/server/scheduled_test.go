package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/scheduled"
)

func TestFuturePostAndPagePublishReturnsDurableScheduledTask(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN",
				Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}}, Fallback: []string{"zh-CN"},
			}, false); err != nil {
				t.Fatal(err)
			}
			contentService := content.NewService(repository)
			var item domain.Post
			if kind == "Page" {
				item, err = contentService.CreatePage(content.CreatePageInput{ID: "future-page", Title: "页面", Markdown: "正文"})
			} else {
				item, err = contentService.CreatePost(content.CreatePostInput{ID: "future-post", Title: "文章", Markdown: "正文"})
			}
			if err != nil {
				t.Fatal(err)
			}
			dueAt := time.Now().UTC().Add(time.Hour)
			visibility := item.Meta.Visibility
			input := content.UpdatePostSettingsInput{
				ExpectedRevision: item.Meta.Revision, Visibility: &visibility, PublishedAt: &dueAt, PublishTimeSet: true,
				Categories: item.Meta.Categories, Tags: item.Meta.Tags, CommentPolicy: item.Meta.CommentPolicy, Template: item.Meta.Template,
			}
			if kind == "Page" {
				item, err = contentService.UpdatePageSettings(item.Meta.ID, input)
			} else {
				item, err = contentService.UpdatePostSettings(item.Meta.ID, input)
			}
			if err != nil {
				t.Fatal(err)
			}
			scheduler := scheduled.NewService(repository, contentService, fakeSitePublisher{}, nil, nil)
			defer scheduler.Close()
			app := &Server{content: contentService, scheduler: scheduler, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			body, _ := json.Marshal(map[string]int{"revision": item.Meta.Revision})
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
					Status string    `json:"status"`
					TaskID string    `json:"taskId"`
					DueAt  time.Time `json:"dueAt"`
				} `json:"build"`
				Translation struct {
					Status string `json:"status"`
				} `json:"translation"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusAccepted || response.Build.Status != "scheduled" || response.Build.TaskID == "" || !response.Build.DueAt.Equal(dueAt) || response.Translation.Status != "deferred" {
				t.Fatalf("scheduled response = status %d, %#v, body %s", recorder.Code, response, recorder.Body.String())
			}
			stored, err := scheduledContentForTest(contentService, kind, item.Meta.ID)
			if err != nil || stored.Meta.ScheduledRevision != item.Meta.Revision {
				t.Fatalf("stored schedule intent = %#v, %v", stored.Meta, err)
			}

			staleBody, _ := json.Marshal(map[string]int{"revision": item.Meta.Revision - 1})
			staleRequest := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(staleBody))
			staleRequest.SetPathValue("id", item.Meta.ID)
			staleRecorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handlePublishPage(staleRecorder, staleRequest)
			} else {
				app.handlePublishPost(staleRecorder, staleRequest)
			}
			if staleRecorder.Code != http.StatusConflict {
				t.Fatalf("stale schedule status = %d, body = %s", staleRecorder.Code, staleRecorder.Body.String())
			}
		})
	}
}

func TestRestoreRevisionInvalidatesScheduledPublicationForPostAndPage(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			app, contentService, scheduler, item, task := scheduledLifecycleFixture(t, kind, "restore-scheduled-"+kind)
			revisions, err := contentService.ListRevisions(kind, item.Meta.ID)
			if err != nil {
				t.Fatal(err)
			}
			revisionID := ""
			for _, revision := range revisions {
				if revision.Revision == 1 {
					revisionID = revision.ID
					break
				}
			}
			if revisionID == "" {
				t.Fatal("initial revision snapshot was not found")
			}
			request := scheduledLifecycleRequest(t, http.MethodPost, item.Meta.ID, item.Meta.Revision)
			request.SetPathValue("revision", revisionID)
			recorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handleRestorePageRevision(recorder, request)
			} else {
				app.handleRestorePostRevision(recorder, request)
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("restore status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			assertScheduledNeedsReview(t, scheduler, task.ID)
			stored, err := scheduledContentForTest(contentService, kind, item.Meta.ID)
			if err != nil || stored.Meta.ScheduledRevision != 0 {
				t.Fatalf("restored content retained stale schedule intent: %#v, %v", stored.Meta, err)
			}
		})
	}
}

func TestLifecycleChangeInvalidatesScheduledPublicationForPostAndPage(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			app, contentService, scheduler, item, task := scheduledLifecycleFixture(t, kind, "recycle-scheduled-"+kind)
			request := scheduledLifecycleRequest(t, http.MethodPost, item.Meta.ID, item.Meta.Revision)
			request.SetPathValue("action", "recycle")
			recorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handlePageLifecycle(recorder, request)
			} else {
				app.handlePostLifecycle(recorder, request)
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("lifecycle status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			assertScheduledNeedsReview(t, scheduler, task.ID)
			stored, err := scheduledContentForTest(contentService, kind, item.Meta.ID)
			if err != nil || stored.Meta.ScheduledRevision != 0 || stored.Meta.Status != domain.ContentStatusRecycled {
				t.Fatalf("recycled content retained stale schedule intent: %#v, %v", stored.Meta, err)
			}
		})
	}
}

func TestPermanentDeleteTerminalizesResidualScheduleForPostAndPage(t *testing.T) {
	for _, kind := range []string{"Post", "Page"} {
		t.Run(kind, func(t *testing.T) {
			app, contentService, scheduler, item, firstTask := scheduledLifecycleFixture(t, kind, "delete-scheduled-"+kind)
			recycled, err := contentService.ChangeStatus(kind, item.Meta.ID, "recycle", item.Meta.Revision)
			if err != nil {
				t.Fatal(err)
			}
			residual, err := scheduler.Start(scheduled.StartInput{
				EntityKind: kind, EntityID: recycled.Meta.ID, Revision: recycled.Meta.Revision, DueAt: *recycled.Meta.PublishedAt,
			})
			if err != nil {
				t.Fatal(err)
			}
			first, err := scheduler.Get(firstTask.ID)
			if err != nil || first.Status != "succeeded" || first.Outcome != "superseded" {
				t.Fatalf("old schedule was not superseded before delete: %#v, %v", first, err)
			}

			request := scheduledLifecycleRequest(t, http.MethodDelete, recycled.Meta.ID, recycled.Meta.Revision)
			recorder := httptest.NewRecorder()
			if kind == "Page" {
				app.handleDeletePage(recorder, request)
			} else {
				app.handleDeletePost(recorder, request)
			}
			var response struct {
				Deleted bool `json:"deleted"`
				Build   struct {
					Status string `json:"status"`
				} `json:"build"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusOK || !response.Deleted || response.Build.Status != "succeeded" {
				t.Fatalf("delete response = status %d, %#v, body = %s", recorder.Code, response, recorder.Body.String())
			}
			assertScheduledNeedsReview(t, scheduler, residual.ID)
			if _, err := scheduledContentForTest(contentService, kind, recycled.Meta.ID); !errors.Is(err, content.ErrNotFound) {
				t.Fatalf("deleted content lookup error = %v, want content.ErrNotFound", err)
			}
		})
	}
}

func scheduledLifecycleFixture(t *testing.T, kind, id string) (*Server, *content.Service, *scheduled.Service, domain.Post, scheduled.Task) {
	t.Helper()
	id = strings.ToLower(id)
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN",
		Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}}, Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	var item domain.Post
	if kind == "Page" {
		item, err = contentService.CreatePage(content.CreatePageInput{ID: id, Title: "计划页面", Markdown: "正文"})
	} else {
		item, err = contentService.CreatePost(content.CreatePostInput{ID: id, Title: "计划文章", Markdown: "正文"})
	}
	if err != nil {
		t.Fatal(err)
	}
	dueAt := time.Now().UTC().Add(time.Hour)
	visibility := item.Meta.Visibility
	pinned := item.Meta.Pinned
	settings := content.UpdatePostSettingsInput{
		ExpectedRevision: item.Meta.Revision, Categories: item.Meta.Categories, Tags: item.Meta.Tags, Cover: item.Meta.Cover,
		Pinned: &pinned, Visibility: &visibility, PublishedAt: &dueAt, PublishTimeSet: true,
		CommentPolicy: item.Meta.CommentPolicy, Template: item.Meta.Template,
	}
	if kind == "Page" {
		item, err = contentService.UpdatePageSettings(item.Meta.ID, settings)
	} else {
		item, err = contentService.UpdatePostSettings(item.Meta.ID, settings)
	}
	if err != nil {
		t.Fatal(err)
	}
	scheduler := scheduled.NewService(repository, contentService, fakeSitePublisher{}, nil, nil)
	t.Cleanup(scheduler.Close)
	task, err := scheduler.Start(scheduled.StartInput{EntityKind: kind, EntityID: item.Meta.ID, Revision: item.Meta.Revision, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	app := &Server{
		content: contentService, scheduler: scheduler, publisher: fakeSitePublisher{},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return app, contentService, scheduler, item, task
}

func scheduledLifecycleRequest(t *testing.T, method, id string, revision int) *http.Request {
	t.Helper()
	body, err := json.Marshal(contentRevisionRequest{Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "/", bytes.NewReader(body))
	request.SetPathValue("id", id)
	return request
}

func assertScheduledNeedsReview(t *testing.T, scheduler *scheduled.Service, id string) {
	t.Helper()
	task, err := scheduler.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "needs-review" || task.Error != "content-changed-before-scheduled-publish" || task.Progress.Percent == 100 {
		t.Fatalf("scheduled task was not terminalized with retained progress: %#v", task)
	}
}

func scheduledContentForTest(service *content.Service, kind, id string) (domain.Post, error) {
	if kind == "Page" {
		return service.GetPage(id)
	}
	return service.GetPost(id)
}
