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
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

type localeProvisionerFunc func(context.Context, string) (localization.Report, error)

func (function localeProvisionerFunc) Provision(ctx context.Context, locale string) (localization.Report, error) {
	return function(ctx, locale)
}

func successfulLocaleProvisioner(_ context.Context, locale string) (localization.Report, error) {
	return localization.Report{Locale: locale, Site: 1, Dictionaries: 56}, nil
}

type recordingLocaleTaskStarter struct {
	inputs []localization.LocaleProvisionStartInput
	launch []string
	failed []string
	err    error
}

func (starter *recordingLocaleTaskStarter) Prepare(input localization.LocaleProvisionStartInput) (localization.Task, bool, error) {
	starter.inputs = append(starter.inputs, localization.LocaleProvisionStartInput{Locales: append([]string(nil), input.Locales...)})
	if starter.err != nil {
		return localization.Task{}, false, starter.err
	}
	buildID, err := publisher.NewBuildTaskID()
	if err != nil {
		return localization.Task{}, false, err
	}
	targets := make([]localization.TargetTask, 0, len(input.Locales))
	for _, locale := range input.Locales {
		targets = append(targets, localization.TargetTask{
			Locale: locale, Status: "queued",
			Progress: taskstore.Progress{Phase: "preparing-site-localization", Total: 1, Message: "preparing-site-localization"},
		})
	}
	return localization.Task{
		SchemaVersion: domain.SchemaVersion,
		ID:            "locale-provision-" + buildID,
		Kind:          "LocaleProvision",
		Operation:     "localize-site",
		Status:        "queued",
		Progress:      taskstore.Progress{Phase: "preparing-site-localization", Total: len(targets) + 2},
		Targets:       targets,
		CreatedAt:     time.Now().UTC(),
	}, true, nil
}

func (starter *recordingLocaleTaskStarter) LaunchPrepared(taskID string) bool {
	starter.launch = append(starter.launch, taskID)
	return true
}

func (starter *recordingLocaleTaskStarter) FailPrepared(taskID, _ string) error {
	starter.failed = append(starter.failed, taskID)
	return nil
}

func TestUpdateLocalesQueuesDurableProvisioningAndReturnsBeforeProvider(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	providerStarted := make(chan struct{})
	providerRelease := make(chan struct{})
	var startedOnce sync.Once
	server := &Server{
		repository: repository,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher:  builds,
		localeProvisioner: localeProvisionerFunc(func(_ context.Context, locale string) (localization.Report, error) {
			startedOnce.Do(func() { close(providerStarted) })
			<-providerRelease
			return localization.Report{Locale: locale, Site: 1}, nil
		}),
		statsCache: make(map[string]cachedPublicStats),
	}
	server.localeTasks = localization.NewTaskService(repository, serverLocaleProvisioner{server: server}, builds)
	server.localeTaskManager = server.localeTasks
	server.localeTasks.SetMutationAcquire(server.acquireBackgroundContentMutation)
	server.localeTasks.SetTargetLifecycleCallback(server.updateLocaleTargetLifecycle)
	t.Cleanup(server.localeTasks.Close)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-cn","enabled":[{"code":"zh-cn","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)

	if recorder.Code != http.StatusAccepted || recorder.Header().Get("X-MutiBlog-Static-Build") != "deferred" {
		t.Fatalf("queued response = %d %q %s", recorder.Code, recorder.Header().Get("X-MutiBlog-Static-Build"), recorder.Body.String())
	}
	var response struct {
		Locales      domain.LocalesConfig `json:"locales"`
		Task         adminTask            `json:"task"`
		Localization struct {
			Status string `json:"status"`
			TaskID string `json:"taskId"`
		} `json:"localization"`
		Build struct {
			Status string `json:"status"`
		} `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Task.Kind != "LocaleProvision" || response.Task.ID == "" || response.Localization.Status != "queued" || response.Localization.TaskID != response.Task.ID || response.Build.Status != "deferred" {
		t.Fatalf("queued response body = %#v", response)
	}
	if len(response.Locales.Enabled) != 2 || response.Locales.Enabled[1].Status != domain.LocaleStatusProvisioning || builds.builds.Load() != 0 {
		t.Fatalf("pre-provider state = locales %#v, builds %d", response.Locales.Enabled, builds.builds.Load())
	}
	select {
	case <-providerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("durable locale task did not start")
	}
	if builds.builds.Load() != 0 {
		t.Fatalf("blocked provider triggered %d builds", builds.builds.Load())
	}
	close(providerRelease)
	task := waitLocaleProvisionTask(t, server.localeTasks, response.Task.ID)
	if task.Status != "succeeded" || task.BuildStatus != "succeeded" || !taskstore.ValidStaticBuildID(task.BuildTaskID) || builds.builds.Load() != 1 {
		t.Fatalf("completed locale task = %#v, builds %d", task, builds.builds.Load())
	}
	locales := readLocaleTestConfig(t, repository)
	if locales.Enabled[1].Status != domain.LocaleStatusReady {
		t.Fatalf("final locale status = %#v", locales.Enabled)
	}
}

func TestUpdateLocalesQueuesEveryNewAndRetryableTargetTogether(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Label: "English", Enabled: true},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusFailed},
			{Code: "de", Label: "Deutsch", Enabled: true, Status: domain.LocaleStatusReady},
		},
		Fallback: []string{"zh-CN"},
	})
	starter := &recordingLocaleTaskStarter{}
	server := &Server{
		repository:        repository,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher:         &countingSitePublisher{},
		localeTaskManager: starter,
		statsCache:        make(map[string]cachedPublicStats),
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true},{"code":"ja","label":"日本語","enabled":true},{"code":"de","label":"Deutsch","enabled":true},{"code":"fr","label":"Français","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)

	if recorder.Code != http.StatusAccepted || len(starter.inputs) != 1 || len(starter.launch) != 1 {
		t.Fatalf("batch response = %d, starts %#v, body %s", recorder.Code, starter.inputs, recorder.Body.String())
	}
	gotTargets := append([]string(nil), starter.inputs[0].Locales...)
	sort.Strings(gotTargets)
	if want := []string{"en", "fr", "ja"}; !equalStrings(gotTargets, want) {
		t.Fatalf("queued targets = %v, want %v", gotTargets, want)
	}
	locales := readLocaleTestConfig(t, repository)
	for _, definition := range locales.Enabled {
		if definition.Code == "de" && definition.Status != domain.LocaleStatusReady {
			t.Fatalf("ready target changed = %#v", definition)
		}
		if (definition.Code == "en" || definition.Code == "ja" || definition.Code == "fr") && definition.Status != domain.LocaleStatusProvisioning {
			t.Fatalf("retry target was not hidden = %#v", definition)
		}
	}
}

func TestUpdateLocalesReceiptFailureDoesNotCommitLocalesOrLegacySiteRepair(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled:       []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}},
		Fallback:      []string{"en"},
	})
	beforeSite := domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Locales:       map[string]domain.LocalizedSite{"en": {Title: "Legacy site"}},
		CreatedAt:     time.Now().UTC().Add(-time.Hour),
		UpdatedAt:     time.Now().UTC().Add(-time.Hour),
	}
	if err := repository.WriteYAML("config/site.yaml", beforeSite, false); err != nil {
		t.Fatal(err)
	}
	starter := &recordingLocaleTaskStarter{err: errors.New("receipt unavailable")}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: &countingSitePublisher{}, localeTaskManager: starter}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"en","label":"English","enabled":true},{"code":"ja","label":"日本語","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)

	if recorder.Code != http.StatusInternalServerError || !bytes.Contains(recorder.Body.Bytes(), []byte("locale_task_write_failed")) {
		t.Fatalf("receipt failure = %d %s", recorder.Code, recorder.Body.String())
	}
	locales := readLocaleTestConfig(t, repository)
	if locales.SourceLocale != "en" || len(locales.Enabled) != 1 || locales.Enabled[0].Code != "en" {
		t.Fatalf("locale rollback = %#v", locales)
	}
	var site domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	if site.SourceLocale != "en" || len(site.Locales) != 1 || site.Locales["en"].Title != "Legacy site" {
		t.Fatalf("site rollback = %#v", site)
	}
}

func TestUpdateLocalesValidationAndNoTargetCompatibility(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		code string
	}{
		{name: "source-switch", body: `{"sourceLocale":"en","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`, code: "source_locale_fixed"},
		{name: "disable-existing", body: `{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":false}]}`, code: "locale_disable_forbidden"},
		{name: "disabled-addition", body: `{"sourceLocale":"zh-CN","enabled":[{"code":"de","label":"Deutsch","enabled":false}]}`, code: "locale_enable_required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := localeTestRepository(t, domain.LocalesConfig{
				SchemaVersion: domain.SchemaVersion,
				SourceLocale:  "zh-CN",
				Enabled: []domain.LocaleDefinition{
					{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
					{Code: "en", Label: "English", Enabled: true, Status: domain.LocaleStatusReady},
				},
				Fallback: []string{"zh-CN"},
			})
			server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: &countingSitePublisher{}}
			recorder := httptest.NewRecorder()
			server.handleUpdateLocales(recorder, httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(test.body)))
			if recorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(recorder.Body.Bytes(), []byte(test.code)) {
				t.Fatalf("validation response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Label: "English", Enabled: true, Status: domain.LocaleStatusReady},
		},
		Fallback: []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds}
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true}]}`)))
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-MutiBlog-Static-Build") != "skipped" || builds.builds.Load() != 0 {
		t.Fatalf("no-target compatibility response = %d, builds %d, body %s", recorder.Code, builds.builds.Load(), recorder.Body.String())
	}
	if locales := readLocaleTestConfig(t, repository); len(locales.Enabled) != 2 || locales.Enabled[1].Status != domain.LocaleStatusReady {
		t.Fatalf("stale omission changed locale membership = %#v", locales.Enabled)
	}
}

func TestLocaleTargetLifecycleCallbackIsAtomicAndRejectsUnknownTargets(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Label: "English", Enabled: true, Status: domain.LocaleStatusProvisioning},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{"zh-CN"},
	})
	server := &Server{repository: repository, statsCache: make(map[string]cachedPublicStats)}
	if err := server.updateLocaleTargetLifecycle(context.Background(), localization.TargetLifecycleUpdate{TaskID: "locale-task", Locales: []string{"en", "ja"}, Status: domain.LocaleStatusBuilding}); err != nil {
		t.Fatal(err)
	}
	if err := server.updateLocaleTargetLifecycle(context.Background(), localization.TargetLifecycleUpdate{TaskID: "locale-task", Locales: []string{"en", "missing"}, Status: domain.LocaleStatusReady}); err == nil {
		t.Fatal("unknown target lifecycle update succeeded")
	}
	locales := readLocaleTestConfig(t, repository)
	if locales.Enabled[1].Status != domain.LocaleStatusBuilding || locales.Enabled[2].Status != domain.LocaleStatusBuilding {
		t.Fatalf("partial lifecycle update escaped = %#v", locales.Enabled)
	}
}

func TestStartupRecoversLocaleTaskBeforeSkippingGenericBuild(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{"zh-CN"},
	})
	seed := localization.NewTaskService(repository, nil, nil)
	task, created, err := seed.Prepare(localization.LocaleProvisionStartInput{Locales: []string{"ja"}})
	seed.Close()
	if err != nil || !created {
		t.Fatalf("prepare interrupted locale task = %#v, %t, %v", task, created, err)
	}
	if err := repository.WriteFile("config/initialized", []byte("ok\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	builds := &countingSitePublisher{}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: builds})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	terminal := waitLocaleProvisionTask(t, app.localeTasks, task.ID)
	if terminal.Status != "failed" || builds.builds.Load() != 0 || !app.initialBuildQueued {
		t.Fatalf("recovered locale task = %#v, builds %d, startupQueued %t", terminal, builds.builds.Load(), app.initialBuildQueued)
	}
}

func waitLocaleProvisionTask(t *testing.T, service *localization.TaskService, id string) localization.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := service.Get(id)
		if err == nil && task.Status != "queued" && task.Status != "running" {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := service.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("locale provision task did not finish: %#v", task)
	return localization.Task{}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func localeTestRepository(t *testing.T, locales domain.LocalesConfig) *fsrepo.Repository {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	site := domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Locales:       map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	return repository
}

func readLocaleTestConfig(t *testing.T, repository *fsrepo.Repository) domain.LocalesConfig {
	t.Helper()
	var locales domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		t.Fatal(err)
	}
	return locales
}
