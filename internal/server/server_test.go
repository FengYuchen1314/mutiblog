package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestSetupLoginSessionAndLogout(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	assertJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/setup/status", nil, http.StatusOK, map[string]any{"initialized": false})
	setup := map[string]string{
		"siteTitle": "MutiBlog Test", "sourceLocale": "zh-cn", "adminLocale": "en",
		"timezone": "Asia/Shanghai", "username": "admin", "password": "correct horse battery staple",
	}
	response := requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	if info, err := os.Stat(filepath.Join(repository.Root(), "config", "admin.yaml")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("admin config mode = %v, err = %v", info.Mode().Perm(), err)
	}

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", setup, nil)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("second setup status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong password"}, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid login status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": setup["password"]}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var session map[string]any
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	csrf := session["csrfToken"].(string)

	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/auth/session", nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/logout", nil, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without csrf status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/auth/logout", nil, map[string]string{"X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.StatusCode)
	}
	response.Body.Close()

	response = requestJSON(t, client, http.MethodGet, testServer.URL+"/api/v1/auth/session", nil, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d", response.StatusCode)
	}
	response.Body.Close()
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertJSON(t *testing.T, client *http.Client, method, url string, body any, status int, want map[string]any) {
	t.Helper()
	response := requestJSON(t, client, method, url, body, nil)
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("status = %d, want %d", response.StatusCode, status)
	}
	var got map[string]any
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %v, want %v", key, got[key], value)
		}
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return string(data)
}
