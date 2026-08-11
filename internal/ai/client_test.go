package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestChatUsesOpenAICompatibleEndpointWithoutLeakingKey(t *testing.T) {
	const secret = "test-secret-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Fatalf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "test-model" {
			t.Fatalf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer server.Close()
	provider := domain.AIProviderConfig{BaseURL: server.URL + "/v1", Model: "test-model"}
	result, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), provider, secret, []ChatMessage{{Role: "user", Content: "ping"}}, 4)
	if err != nil || result != "OK" {
		t.Fatalf("result = %q, err = %v", result, err)
	}
}

func TestChatRejectsInvalidProviderAndRedactsResponseBody(t *testing.T) {
	if _, err := (Client{}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: "file:///tmp", Model: "x"}, "secret", nil, 4); err == nil {
		t.Fatal("invalid provider unexpectedly accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("server echoed super-secret-value"))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "super-secret-value", nil, 4)
	if err == nil || strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestChatRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]} {"unexpected":true}`))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "secret", nil, 4)
	if !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("trailing response error = %v", err)
	}
}

func TestProviderRetryabilityOnlyIncludesTransientFailures(t *testing.T) {
	status := http.StatusUnauthorized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer server.Close()
	provider := domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), provider, "secret", nil, 4)
	if RetryableProviderError(err) {
		t.Fatalf("HTTP 401 must not be retried: %v", err)
	}
	status = http.StatusTooManyRequests
	_, err = (Client{HTTPClient: server.Client()}).Chat(context.Background(), provider, "secret", nil, 4)
	if !RetryableProviderError(err) {
		t.Fatalf("HTTP 429 should be retried: %v", err)
	}
}
