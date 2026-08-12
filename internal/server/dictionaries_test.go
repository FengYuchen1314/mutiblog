package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestFrameworkDictionaryRejectsDirectUpdates(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}, {Code: "fr", Label: "Français", Enabled: true, Status: domain.LocaleStatusReady}}}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := dictionary.Ensure(repository); err != nil {
		t.Fatal(err)
	}
	server := &Server{repository: repository, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), publisher: fakeSitePublisher{}}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/dictionaries/fr", bytes.NewBufferString(`{"values":{"home":"Accueil"}}`))
	request.SetPathValue("locale", "fr")
	recorder := httptest.NewRecorder()
	server.handleUpdateFrameworkDictionary(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"dictionary_managed"`)) {
		t.Fatalf("managed target response = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/dictionaries/zh-CN", bytes.NewBufferString(`{"values":{"home":"首页自定义"}}`))
	request.SetPathValue("locale", "zh-CN")
	recorder = httptest.NewRecorder()
	server.handleUpdateFrameworkDictionary(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"dictionary_managed"`)) || recorder.Header().Get("X-MutiBlog-Static-Build") != "" {
		t.Fatalf("managed source response = %d %s", recorder.Code, recorder.Body.String())
	}
}
