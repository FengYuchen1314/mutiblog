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

func TestChatSeparatesMissingKeyFromInvalidProvider(t *testing.T) {
	if _, err := (Client{}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: "file:///tmp", Model: "x"}, "secret", nil, 4); !errors.Is(err, ErrInvalidProvider) || errors.Is(err, ErrKeyMissing) {
		t.Fatalf("invalid provider error = %v", err)
	}
	if _, err := (Client{}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: "https://provider.test/v1", Model: "x"}, " \t ", nil, 4); !errors.Is(err, ErrKeyMissing) || errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestChatReportsSafeRedactedJSONProviderError(t *testing.T) {
	const secret = "super-secret-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication failed; api_key=super-secret-value; Bearer \"secondary!token\"; token=secondary-token; secret: backup-secret; client_secret=\"client!secret\"; credential=opaque!credential; password: 'password!value'; alternate sk-other-secret-value; request scope denied"}}`))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, secret, nil, 4)
	if !errors.Is(err, ErrProviderFailed) || RetryableProviderError(err) {
		t.Fatalf("provider error = %v", err)
	}
	message := err.Error()
	for _, unsafe := range []string{secret, "secondary!token", "secondary-token", "backup-secret", "client!secret", "opaque!credential", "password!value", "sk-other-secret-value"} {
		if strings.Contains(message, unsafe) {
			t.Fatalf("unsafe error = %v", err)
		}
	}
	if !strings.Contains(message, "HTTP 401") || !strings.Contains(message, "request scope denied") || !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("missing safe provider summary = %v", err)
	}
}

func TestChatReportsSafePlainTextProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Rate limit reached for project alpha; retry later"))
	}))
	defer server.Close()
	_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "secret", nil, 4)
	if !errors.Is(err, ErrProviderFailed) || !RetryableProviderError(err) || !strings.Contains(err.Error(), "Rate limit reached for project alpha") {
		t.Fatalf("plain-text provider error = %v", err)
	}
}

func TestChatRejectsHTMLBinaryAndOversizedProviderErrors(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		body        string
		privateText string
	}{
		{name: "html", contentType: "text/plain", body: "upstream prefix: <html><body>private HTML diagnostic", privateText: "private HTML diagnostic"},
		{name: "truncated-html", contentType: "text/plain", body: "upstream prefix: <html private HTML diagnostic", privateText: "private HTML diagnostic"},
		{name: "binary", contentType: "text/plain", body: "binary\x00private diagnostic", privateText: "private diagnostic"},
		{name: "unicode-format", contentType: "text/plain", body: "safe prefix \u202eprivate diagnostic", privateText: "private diagnostic"},
		{name: "binary-media-type", contentType: "application/octet-stream", body: "printable private diagnostic", privateText: "private diagnostic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: server.URL, Model: "x"}, "secret", nil, 4)
			if !errors.Is(err, ErrProviderFailed) || !RetryableProviderError(err) || strings.Contains(err.Error(), test.privateText) {
				t.Fatalf("unsafe provider error = %v", err)
			}
		})
	}
	boundedSummary := providerErrorSummary(strings.NewReader(strings.Repeat("a", maxProviderErrorSummaryRunes+40)), "text/plain", "secret")
	if runes := []rune(boundedSummary); len(runes) != maxProviderErrorSummaryRunes || runes[len(runes)-1] != '…' {
		t.Fatalf("provider error summary length = %d, value = %q", len([]rune(boundedSummary)), boundedSummary)
	}

	oversized := strings.NewReader(strings.Repeat("oversized private diagnostic ", maxProviderErrorBodyBytes))
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(oversized),
			Request:    request,
		}, nil
	})}
	_, err := (Client{HTTPClient: client}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: "https://provider.test/v1", Model: "x"}, "secret", nil, 4)
	if !errors.Is(err, ErrProviderFailed) || !RetryableProviderError(err) || strings.Contains(err.Error(), "oversized private diagnostic") {
		t.Fatalf("unsafe error = %v", err)
	}
	if oversized.Len() == 0 {
		t.Fatal("provider error body was read without a bound")
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
	for _, test := range []struct {
		status    int
		retryable bool
	}{
		{status: http.StatusBadRequest, retryable: false},
		{status: http.StatusUnauthorized, retryable: false},
		{status: http.StatusRequestTimeout, retryable: true},
		{status: http.StatusTooManyRequests, retryable: true},
		{status: http.StatusInternalServerError, retryable: true},
	} {
		status = test.status
		_, err := (Client{HTTPClient: server.Client()}).Chat(context.Background(), provider, "secret", nil, 4)
		if got := RetryableProviderError(err); got != test.retryable {
			t.Fatalf("HTTP %d retryable = %t, want %t: %v", test.status, got, test.retryable, err)
		}
	}
}
