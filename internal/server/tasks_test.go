package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/projection"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/scheduled"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

func TestAdminTaskAggregationIncludesEverySupportedKindAndRejectsUnknownHeaders(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	buildID := "20260811T010203.000000000Z-aabbccdd"
	indexTaskID := "index-rebuild-20260811T010203.000000000Z-aabbccdd"
	localeTaskID := "locale-provision-20260811T010203.000000000Z-aabbccdd"
	records := map[string]any{
		"translation-example": translation.Task{
			SchemaVersion: domain.SchemaVersion, ID: "translation-example", Kind: "Translation", EntityKind: "Post", EntityID: "post-one",
			PublicationRevision: 2, Status: "running", Progress: taskstore.Progress{Phase: "chunks", Current: 2, Total: 5, Percent: 36},
			BuildStatus: "running", BuildTaskID: buildID, CreatedAt: now.Add(-time.Minute),
		},
		buildID: publisher.Task{
			SchemaVersion: domain.SchemaVersion, ID: buildID, Kind: "StaticBuild", Operation: "publish", SubjectKind: "Post", SubjectID: "post-one",
			Status: "running", Progress: taskstore.Progress{Phase: "render", Current: 1, Total: 4, Percent: 25}, CreatedAt: now,
		},
		"backup-task-example": backup.Task{
			SchemaVersion: domain.SchemaVersion, ID: "backup-task-example", Kind: "Backup", Operation: "create", Status: "queued",
			Progress: taskstore.Progress{Phase: "queued", Total: 4}, CreatedAt: now.Add(-2 * time.Minute),
		},
		"scheduled-publish-20260811T010203.000000000Z-aabbccdd": scheduled.Task{
			SchemaVersion: domain.SchemaVersion, ID: "scheduled-publish-20260811T010203.000000000Z-aabbccdd", Kind: "ScheduledPublish", Operation: "publish",
			EntityKind: "Page", EntityID: "page-one", Revision: 3, DueAt: now.Add(time.Hour), Status: "succeeded",
			Progress: taskstore.Progress{Phase: "completed", Current: 4, Total: 4, Percent: 100}, CreatedAt: now.Add(-3 * time.Minute),
			BuildStatus: "failed", BuildTaskID: buildID, TranslationStatus: "failed", TranslationTaskID: "translation-example", Outcome: "rebuild-failed",
		},
		indexTaskID: projection.Task{
			SchemaVersion: domain.SchemaVersion, ID: indexTaskID, Kind: "IndexRebuild", Operation: "rebuild-search-index",
			Status: "running", Progress: taskstore.Progress{Phase: "index-rebuild", Current: 1, Total: 3, Percent: 25}, CreatedAt: now.Add(-30 * time.Second),
		},
		localeTaskID: localization.Task{
			SchemaVersion: domain.SchemaVersion, ID: localeTaskID, Kind: "LocaleProvision", Operation: "localize-site", Status: "running",
			Progress: taskstore.Progress{Phase: "building-localized-site", Current: 1, Total: 3, Percent: 78}, CreatedAt: now.Add(-15 * time.Second),
			Targets:     []localization.TargetTask{{Locale: "ja", Status: "succeeded", Attempts: 1, Progress: taskstore.Progress{Phase: "completed", Current: 1, Total: 1, Percent: 100}}},
			BuildStatus: "queued", BuildTaskID: buildID,
		},
	}
	for id, record := range records {
		if err := repository.WriteYAML("state/tasks/"+id+".yaml", record, false); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := scheduled.NewService(repository, nil, nil, nil, nil)
	defer scheduler.Close()
	projectionService, err := projection.Open(repository, content.NewService(repository), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer projectionService.Close()
	app := &Server{
		translator:  translation.NewService(repository, nil, nil, nil),
		backups:     backup.NewService(repository),
		publisher:   newTrackedSitePublisher(publisher.NewService(repository, nil, nil)),
		scheduler:   scheduler,
		projection:  projectionService,
		localeTasks: localization.NewTaskService(repository, nil, nil),
	}
	defer app.localeTasks.Close()
	items, err := app.listAdminTasks()
	if err != nil || len(items) != 6 {
		t.Fatalf("listAdminTasks() = %#v, %v", items, err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Kind] = true
		if item.Kind == "Translation" && (item.Operation != "publish-translate" || item.BuildStatus != "running" || item.BuildTaskID != buildID) {
			t.Fatalf("translation child build metadata was not preserved: %#v", item)
		}
		if item.Kind == "ScheduledPublish" && (item.BuildStatus != "failed" || item.BuildTaskID != buildID || item.TranslationStatus != "failed" || item.TranslationTaskID != "translation-example" || item.Outcome != "published-with-warning") {
			t.Fatalf("scheduled child warning metadata was not preserved: %#v", item)
		}
		if item.Kind == "LocaleProvision" {
			targets, ok := item.Targets.([]localization.TargetTask)
			if !ok || len(targets) != 1 || targets[0].Locale != "ja" || item.BuildStatus != "queued" || item.BuildTaskID != buildID {
				t.Fatalf("locale provisioning task metadata was not preserved: %#v", item)
			}
		}
	}
	for _, kind := range []string{"Translation", "StaticBuild", "Backup", "ScheduledPublish", "IndexRebuild", "LocaleProvision"} {
		if !seen[kind] {
			t.Fatalf("missing %s task in %#v", kind, items)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks/"+localeTaskID, nil)
	request.SetPathValue("id", localeTaskID)
	recorder := httptest.NewRecorder()
	app.handleGetTask(recorder, request)
	var fetched adminTask
	if recorder.Code != http.StatusOK {
		t.Fatalf("get locale provision task = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.ID != localeTaskID || fetched.Kind != "LocaleProvision" || fetched.BuildTaskID != buildID {
		t.Fatalf("fetched locale provision task = %#v", fetched)
	}
	if err := repository.WriteYAML("state/tasks/unknown.yaml", map[string]any{
		"schemaVersion": domain.SchemaVersion, "id": "unknown", "kind": "Unknown",
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := app.listAdminTasks(); err == nil {
		t.Fatal("aggregate task list accepted an unknown shared task header")
	}
}
