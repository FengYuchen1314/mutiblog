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

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type localeBuildPublisher struct {
	request publisher.BuildRequest
	err     error
}

type localeProvisionerFunc func(context.Context, string) (localization.Report, error)
type localePublisherFunc func(context.Context) (publisher.BuildReport, error)

func (function localeProvisionerFunc) Provision(ctx context.Context, locale string) (localization.Report, error) {
	return function(ctx, locale)
}

func (function localePublisherFunc) Build(ctx context.Context) (publisher.BuildReport, error) {
	return function(ctx)
}

func successfulLocaleProvisioner(_ context.Context, locale string) (localization.Report, error) {
	return localization.Report{Locale: locale, Site: 1, Dictionaries: 56}, nil
}

func (p *localeBuildPublisher) Build(ctx context.Context) (publisher.BuildReport, error) {
	p.request = publisher.BuildRequestFromContext(ctx)
	return publisher.BuildReport{SchemaVersion: domain.SchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}, p.err
}

func TestUpdateLocalesKeepsFixedChineseSourceAndAppendsTargets(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true}},
		Fallback:      []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds, localeProvisioner: localeProvisionerFunc(successfulLocaleProvisioner)}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-cn","enabled":[{"code":"zh-cn","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
	recorder := httptest.NewRecorder()

	server.handleUpdateLocales(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Header().Get("X-MutiBlog-Static-Build") != "succeeded" {
		t.Fatalf("append response = %d %s", recorder.Code, recorder.Body.String())
	}
	locales := readLocaleTestConfig(t, repository)
	if locales.SourceLocale != "zh-CN" || len(locales.Enabled) != 2 || builds.builds.Load() != 1 {
		t.Fatalf("appended locales = %#v, builds = %d", locales, builds.builds.Load())
	}
	if locales.Enabled[0].Code != "zh-CN" || locales.Enabled[0].Status != domain.LocaleStatusReady || locales.Enabled[1].Code != "en" || locales.Enabled[1].Status != domain.LocaleStatusReady {
		t.Fatalf("appended locale statuses = %#v", locales.Enabled)
	}
	var site domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		t.Fatal(err)
	}
	if site.SourceLocale != "zh-CN" {
		t.Fatalf("site source locale = %q", site.SourceLocale)
	}
}

func TestUpdateLocalesProvisionsStatuslessLegacyTargetBeforeMarkingReady(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Label: "English", Enabled: true},
		},
		Fallback: []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	provisioned := ""
	statusAtProvision := ""
	server := &Server{
		repository: repository,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher:  builds,
		localeProvisioner: localeProvisionerFunc(func(_ context.Context, locale string) (localization.Report, error) {
			provisioned = locale
			persisted := readLocaleTestConfig(t, repository)
			for _, definition := range persisted.Enabled {
				if definition.Code == locale {
					statusAtProvision = definition.Status
				}
			}
			return localization.Report{Locale: locale}, nil
		}),
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
	recorder := httptest.NewRecorder()

	server.handleUpdateLocales(recorder, request)

	locales := readLocaleTestConfig(t, repository)
	if recorder.Code != http.StatusOK || provisioned != "en" || statusAtProvision != domain.LocaleStatusProvisioning || locales.Enabled[1].Status != domain.LocaleStatusReady || builds.builds.Load() != 1 {
		t.Fatalf("legacy target upgrade = status %d, provisioned %q at %q, locales %#v, builds %d", recorder.Code, provisioned, statusAtProvision, locales.Enabled, builds.builds.Load())
	}
}

func TestUpdateLocalesPersistsAllRetryTargetsAsProvisioningBeforeWork(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "en", Label: "English", Enabled: true},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusFailed},
		},
		Fallback: []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	statusesAtFirstProvision := make(map[string]string)
	provisionCalls := 0
	server := &Server{
		repository: repository,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher:  builds,
		localeProvisioner: localeProvisionerFunc(func(_ context.Context, locale string) (localization.Report, error) {
			provisionCalls++
			if provisionCalls == 1 {
				persisted := readLocaleTestConfig(t, repository)
				for _, definition := range persisted.Enabled {
					statusesAtFirstProvision[definition.Code] = definition.Status
				}
			}
			return localization.Report{Locale: locale}, nil
		}),
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true},{"code":"ja","label":"日本語","enabled":true}]}`))
	recorder := httptest.NewRecorder()

	server.handleUpdateLocales(recorder, request)

	locales := readLocaleTestConfig(t, repository)
	if recorder.Code != http.StatusOK || provisionCalls != 2 || builds.builds.Load() != 1 {
		t.Fatalf("retry response = %d, provisions %d, builds %d, body %s", recorder.Code, provisionCalls, builds.builds.Load(), recorder.Body.String())
	}
	if statusesAtFirstProvision["en"] != domain.LocaleStatusProvisioning || statusesAtFirstProvision["ja"] != domain.LocaleStatusProvisioning {
		t.Fatalf("persisted retry statuses = %#v", statusesAtFirstProvision)
	}
	if locales.Enabled[1].Status != domain.LocaleStatusReady || locales.Enabled[2].Status != domain.LocaleStatusReady {
		t.Fatalf("completed retry statuses = %#v", locales.Enabled)
	}
}

func TestUpdateLocalesRejectsSourceSwitchAndDisableButIgnoresOmission(t *testing.T) {
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
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds, localeProvisioner: localeProvisionerFunc(successfulLocaleProvisioner)}

	for _, test := range []struct {
		name string
		body string
		code string
	}{
		{name: "source-switch", body: `{"sourceLocale":"en","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`, code: "source_locale_fixed"},
		{name: "disable-existing", body: `{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":false}]}`, code: "locale_disable_forbidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(test.body))
			recorder := httptest.NewRecorder()
			server.handleUpdateLocales(recorder, request)
			if recorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(recorder.Body.Bytes(), []byte(test.code)) {
				t.Fatalf("rejected response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true}]}`))
	recorder := httptest.NewRecorder()
	server.handleUpdateLocales(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stale omission response = %d %s", recorder.Code, recorder.Body.String())
	}
	locales := readLocaleTestConfig(t, repository)
	if len(locales.Enabled) != 2 || !definitionsEnabled(locales.Enabled, "en") || locales.Enabled[1].Status != domain.LocaleStatusReady {
		t.Fatalf("stale omission removed an existing locale: %#v", locales.Enabled)
	}
	if builds.builds.Load() != 1 {
		t.Fatalf("accepted locale updates triggered %d builds, want 1", builds.builds.Load())
	}
}

func TestUpdateLocalesMergesStaleAdditionsAndCanonicalDuplicates(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds, localeProvisioner: localeProvisionerFunc(successfulLocaleProvisioner)}

	for _, body := range []string{
		`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"ja","label":"日本語","enabled":true}]}`,
		`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"fr","label":"Français","enabled":true}]}`,
		`{"sourceLocale":"zh-cn","enabled":[{"code":"JA","label":"stale label","enabled":true},{"code":"ja","label":"duplicate label","enabled":true}]}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(body))
		recorder := httptest.NewRecorder()
		server.handleUpdateLocales(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("stale append response = %d %s", recorder.Code, recorder.Body.String())
		}
	}

	locales := readLocaleTestConfig(t, repository)
	if len(locales.Enabled) != 3 || locales.Enabled[0].Code != "zh-CN" || locales.Enabled[1].Code != "ja" || locales.Enabled[2].Code != "fr" {
		t.Fatalf("merged locales = %#v", locales.Enabled)
	}
	if locales.Enabled[1].Label != "日本語" || locales.Enabled[1].Status != domain.LocaleStatusReady || locales.Enabled[2].Status != domain.LocaleStatusReady {
		t.Fatalf("canonical duplicate changed stored locale = %#v", locales.Enabled)
	}
	if builds.builds.Load() != 3 {
		t.Fatalf("accepted locale updates triggered %d builds, want 3", builds.builds.Load())
	}
}

func TestUpdateLocalesRejectsDisabledAddition(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: &countingSitePublisher{}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"de","label":"Deutsch","enabled":false}]}`))
	recorder := httptest.NewRecorder()

	server.handleUpdateLocales(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(recorder.Body.Bytes(), []byte("locale_enable_required")) {
		t.Fatalf("disabled addition response = %d %s", recorder.Code, recorder.Body.String())
	}
	if locales := readLocaleTestConfig(t, repository); len(locales.Enabled) != 1 {
		t.Fatalf("disabled addition changed locales = %#v", locales.Enabled)
	}
}

func TestUpdateLocalesKeepsFailedLocaleWithoutPublishingIt(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	builds := &countingSitePublisher{}
	server := &Server{
		repository: repository,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher:  builds,
		localeProvisioner: localeProvisionerFunc(func(context.Context, string) (localization.Report, error) {
			return localization.Report{}, errors.New("provider unavailable")
		}),
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString("{\"sourceLocale\":\"zh-CN\",\"enabled\":[{\"code\":\"zh-CN\",\"label\":\"简体中文\",\"enabled\":true},{\"code\":\"ja\",\"label\":\"日本語\",\"enabled\":true}]}"))
	recorder := httptest.NewRecorder()

	server.handleUpdateLocales(recorder, request)

	if recorder.Code != http.StatusAccepted || recorder.Header().Get("X-MutiBlog-Static-Build") != "skipped" {
		t.Fatalf("failed localization response = %d %q %s", recorder.Code, recorder.Header().Get("X-MutiBlog-Static-Build"), recorder.Body.String())
	}
	locales := readLocaleTestConfig(t, repository)
	if len(locales.Enabled) != 2 || locales.Enabled[1].Status != domain.LocaleStatusFailed || !locales.Enabled[1].Enabled {
		t.Fatalf("failed locale was not retained = %#v", locales.Enabled)
	}
	if builds.builds.Load() != 0 {
		t.Fatalf("failed locale triggered %d public builds", builds.builds.Load())
	}
}

func TestUpdateLocalesBuildsSuccessfulTargetsOnceWhenAnotherTargetFails(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	builds := 0
	statusesAtBuild := make(map[string]string)
	server := &Server{
		repository: repository,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		publisher: localePublisherFunc(func(context.Context) (publisher.BuildReport, error) {
			builds++
			for _, definition := range readLocaleTestConfig(t, repository).Enabled {
				statusesAtBuild[definition.Code] = definition.Status
			}
			return publisher.BuildReport{SchemaVersion: domain.SchemaVersion}, nil
		}),
		localeProvisioner: localeProvisionerFunc(func(_ context.Context, locale string) (localization.Report, error) {
			if locale == "fr" {
				return localization.Report{}, errors.New("synthetic French failure")
			}
			return localization.Report{Locale: locale}, nil
		}),
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"ja","label":"日本語","enabled":true},{"code":"fr","label":"Français","enabled":true}]}`))
	recorder := httptest.NewRecorder()

	server.handleUpdateLocales(recorder, request)

	locales := readLocaleTestConfig(t, repository)
	if recorder.Code != http.StatusAccepted || builds != 1 || locales.Enabled[1].Status != domain.LocaleStatusReady || locales.Enabled[2].Status != domain.LocaleStatusFailed {
		t.Fatalf("partial localization = status %d, locales %#v, builds %d, body %s", recorder.Code, locales.Enabled, builds, recorder.Body.String())
	}
	if statusesAtBuild["ja"] != domain.LocaleStatusBuilding || statusesAtBuild["fr"] != domain.LocaleStatusFailed {
		t.Fatalf("locale statuses at build = %#v", statusesAtBuild)
	}
}

func TestUpdateLocalesReturnsSavedConfigWhenSameRequestBuildFails(t *testing.T) {
	repository := localeTestRepository(t, domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled:       []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}},
		Fallback:      []string{"zh-CN"},
	})
	builds := &localeBuildPublisher{err: errors.New("renderer unavailable")}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: builds, localeProvisioner: localeProvisionerFunc(successfulLocaleProvisioner)}
	handler := server.withBuildTaskRequest(http.HandlerFunc(server.handleUpdateLocales))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/locales", bytes.NewBufferString(`{"sourceLocale":"zh-CN","enabled":[{"code":"zh-CN","label":"简体中文","enabled":true},{"code":"en","label":"English","enabled":true}]}`))
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
	if response.Locales.SourceLocale != "zh-CN" || len(response.Locales.Enabled) != 2 || response.Locales.Enabled[1].Status != domain.LocaleStatusFailed || response.Build.Status != "failed" {
		t.Fatalf("failed build response body = %#v", response)
	}
	if builds.request.TaskID != "locale-build-123" || builds.request.Operation != "localization-rebuild" {
		t.Fatalf("build request = %#v", builds.request)
	}
	locales := readLocaleTestConfig(t, repository)
	if locales.SourceLocale != "zh-CN" || len(locales.Enabled) != 2 || locales.Enabled[1].Status != domain.LocaleStatusFailed {
		t.Fatalf("saved locales = %#v", locales)
	}
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
