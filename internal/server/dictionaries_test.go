package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestFrameworkDictionaryCompletenessAndUpdate(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}, {Code: "fr", Label: "Français", Enabled: true}}}
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
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-MutiBlog-Static-Build") != "succeeded" {
		t.Fatalf("update response = %d %s", recorder.Code, recorder.Body.String())
	}
	var updated frameworkDictionaryView
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Locale != "fr" || updated.Values["home"] != "Accueil" || updated.Translated != 1 || len(updated.Missing) != updated.Total-1 {
		t.Fatalf("updated framework dictionary = %#v", updated)
	}
}
