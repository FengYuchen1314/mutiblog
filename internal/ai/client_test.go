package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

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
		if _, ok := body["thinking"]; ok {
			t.Fatalf("generic OpenAI-compatible request unexpectedly included thinking: %#v", body["thinking"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	provider := domain.AIProviderConfig{BaseURL: server.URL + "/v1", Model: "test-model"}
	result, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), provider, secret, []ChatMessage{{Role: "user", Content: "ping"}}, 4)
	if err != nil || result != "OK" {
		t.Fatalf("result = %q, err = %v", result, err)
	}
}

func TestChatDisablesThinkingForOfficialDeepSeekV4(t *testing.T) {
	var body map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.deepseek.com/chat/completions" {
			t.Fatalf("endpoint = %s", request.URL)
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)),
			Request:    request,
		}, nil
	})}

	provider := domain.AIProviderConfig{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	result, err := (Client{HTTPClient: client}).Chat(context.Background(), provider, "test-key", []ChatMessage{{Role: "user", Content: "ping"}}, 8)
	if err != nil || result != "OK" {
		t.Fatalf("result = %q, err = %v", result, err)
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", body["thinking"])
	}
}

func TestDeepSeekThinkingModeDoesNotLeakToOtherProviders(t *testing.T) {
	tests := []domain.AIProviderConfig{
		{BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
		{BaseURL: "https://proxy.example.com", Model: "deepseek-v4-flash"},
		{BaseURL: "https://api.deepseek.com.evil", Model: "deepseek-v4-flash"},
		{BaseURL: "not a URL", Model: "deepseek-v4-flash"},
	}
	for _, provider := range tests {
		if mode := disabledThinkingMode(provider); mode != nil {
			t.Fatalf("provider %#v unexpectedly got thinking mode %#v", provider, mode)
		}
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
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]} {"unexpected":true}`))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "secret", nil, 4)
	if !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("trailing response error = %v", err)
	}
}

func TestChatRejectsTruncatedSuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"partial translation"},"finish_reason":"length"}]}`))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "secret", nil, 4)
	if !errors.Is(err, ErrProviderFailed) || RetryableProviderError(err) {
		t.Fatalf("truncated response error = %v", err)
	}
}

func TestChatRejectsEmptyTruncatedResponseWithoutRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "secret", nil, 4)
	if !errors.Is(err, ErrProviderFailed) || RetryableProviderError(err) {
		t.Fatalf("empty truncated response error = %v", err)
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
