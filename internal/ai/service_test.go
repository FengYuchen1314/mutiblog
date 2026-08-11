package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestProviderPersistenceMaskingAndConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer server.Close()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{HTTPClient: server.Client()})
	key := "temporary-test-secret"
	view, err := service.Upsert("test-provider", UpsertProviderInput{Name: "Test", BaseURL: server.URL + "/v1", Model: "test-model", Enabled: true, Default: true, APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasKey || view.MaskedKey != "••••cret" {
		t.Fatalf("view = %#v", view)
	}
	info, err := os.Stat(filepath.Join(repository.Root(), "config", "secrets.yaml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %v, err = %v", info.Mode().Perm(), err)
	}
	result, err := service.Test(context.Background(), "test-provider")
	if err != nil || result.Response != "OK" {
		t.Fatalf("test result = %#v, err = %v", result, err)
	}
	views, err := service.List()
	if err != nil || len(views) != 1 || views[0].MaskedKey != "••••cret" {
		t.Fatalf("views = %#v, err = %v", views, err)
	}
	if err := service.Delete("test-provider"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.DefaultCredentials(); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("default credentials error = %v", err)
	}
}
