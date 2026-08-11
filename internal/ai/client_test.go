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
	"time"

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

func TestGoogleTranslateUsesKeylessFormEndpointAndJoinsVerifiedSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/translate_a/single" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("google-free leaked authorization header %q", authorization)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("client") != "gtx" || r.Form.Get("dt") != "t" || r.Form.Get("sl") != "zh-CN" || r.Form.Get("tl") != "en" || r.Form.Get("q") != "你好世界" {
			t.Fatalf("form = %#v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[["Hello ","你好",null,null,10],["world","世界",null,null,10]],null,"zh-CN"]`))
	}))
	defer server.Close()
	provider := domain.AIProviderConfig{Kind: ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single", Model: GoogleFreeDefaultModel, TimeoutSeconds: 5}
	translated, err := (Client{HTTPClient: server.Client()}).GoogleTranslate(context.Background(), provider, "zh-CN", "en", "你好世界")
	if err != nil || translated != "Hello world" {
		t.Fatalf("GoogleTranslate() = %q, %v", translated, err)
	}
}

func TestGoogleTranslateRejectsMalformedTruncatedAndUnexpectedSuccessfulResponses(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		input     string
		retryable bool
	}{
		{name: "source-echo-truncated", status: http.StatusOK, body: `[[["Hello","你"]]]`, retryable: false},
		{name: "later-segment-translation-empty", status: http.StatusOK, body: `[[["Hello","你好"],["","世界"]]]`, input: "你好世界", retryable: true},
		{name: "segment-has-no-echo", status: http.StatusOK, body: `[[["Hello"]]]`, retryable: false},
		{name: "trailing-json", status: http.StatusOK, body: `[[["Hello","你好"]]] {}`, retryable: false},
		{name: "empty-translation", status: http.StatusOK, body: `[[["","你好"]]]`, retryable: true},
		{name: "partial-content", status: http.StatusPartialContent, body: `[[["Hello","你好"]]]`, retryable: false},
		{name: "no-content", status: http.StatusNoContent, body: ``, retryable: false},
		{name: "bad-request", status: http.StatusBadRequest, body: `private source echo`, retryable: false},
		{name: "payload-too-large", status: http.StatusRequestEntityTooLarge, body: `private source echo`, retryable: false},
		{name: "request-timeout", status: http.StatusRequestTimeout, retryable: true},
		{name: "rate-limited", status: http.StatusTooManyRequests, retryable: true},
		{name: "upstream-unavailable", status: http.StatusServiceUnavailable, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			provider := domain.AIProviderConfig{Kind: ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single", Model: GoogleFreeDefaultModel, TimeoutSeconds: 5}
			input := test.input
			if input == "" {
				input = "你好"
			}
			_, err := (Client{HTTPClient: server.Client()}).GoogleTranslate(context.Background(), provider, "zh-CN", "en", input)
			if !errors.Is(err, ErrProviderFailed) || RetryableProviderError(err) != test.retryable {
				t.Fatalf("error = %v, retryable = %t", err, RetryableProviderError(err))
			}
		})
	}
}

func TestGoogleTranslateBoundsRawEncodedAndResponseSizes(t *testing.T) {
	provider := domain.AIProviderConfig{Kind: ProviderKindGoogleFree, BaseURL: "https://translate.example/translate_a/single", Model: GoogleFreeDefaultModel}
	client := Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("oversized input reached the network")
		return nil, nil
	})}}
	if _, err := client.GoogleTranslate(context.Background(), provider, "zh-CN", "en", strings.Repeat("字", GoogleFreeMaxInputRunes+1)); !errors.Is(err, ErrTranslationInputTooLong) {
		t.Fatalf("raw length error = %v", err)
	}
	if _, err := client.GoogleTranslate(context.Background(), provider, "zh-CN", "en", strings.Repeat("😀", GoogleFreeMaxInputRunes)); !errors.Is(err, ErrTranslationInputTooLong) {
		t.Fatalf("encoded length error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", googleFreeMaxResponseBytes+1)))
	}))
	defer server.Close()
	provider.BaseURL = server.URL + "/translate_a/single"
	if _, err := (Client{HTTPClient: server.Client()}).GoogleTranslate(context.Background(), provider, "zh-CN", "en", "你好"); !errors.Is(err, ErrProviderFailed) || !RetryableProviderError(err) {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestGoogleTranslateAppliesProviderTimeoutToInjectedClient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 4*time.Second || remaining > 5*time.Second {
			t.Fatalf("request deadline remaining = %s, present = %t", remaining, ok)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[[["Hello","你好"]]]`)),
			Request:    request,
		}, nil
	})}
	provider := domain.AIProviderConfig{Kind: ProviderKindGoogleFree, BaseURL: "https://translate.example/translate_a/single", Model: GoogleFreeDefaultModel, TimeoutSeconds: 5}
	if translated, err := (Client{HTTPClient: client}).GoogleTranslate(context.Background(), provider, "zh-CN", "en", "你好"); err != nil || translated != "Hello" {
		t.Fatalf("GoogleTranslate() = %q, %v", translated, err)
	}
}

func TestGoogleTranslateRefusesCrossOriginRedirectWithRequestBody(t *testing.T) {
	redirectReached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectReached = true
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/capture")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider := domain.AIProviderConfig{Kind: ProviderKindGoogleFree, BaseURL: source.URL + "/translate_a/single", Model: GoogleFreeDefaultModel, TimeoutSeconds: 5}
	_, err := (Client{HTTPClient: source.Client()}).GoogleTranslate(context.Background(), provider, "zh-CN", "en", "尚未发布的文章")
	if !errors.Is(err, ErrProviderFailed) || RetryableProviderError(err) || redirectReached {
		t.Fatalf("redirect error = %v, target reached = %t", err, redirectReached)
	}
}

func TestGoogleTranslateDoesNotExposeProviderBodyThatMayEchoSource(t *testing.T) {
	const unpublished = "private-unpublished-article"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid q=" + unpublished))
	}))
	defer server.Close()
	provider := domain.AIProviderConfig{Kind: ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single", Model: GoogleFreeDefaultModel, TimeoutSeconds: 5}
	_, err := (Client{HTTPClient: server.Client()}).GoogleTranslate(context.Background(), provider, "zh-CN", "en", unpublished)
	if !errors.Is(err, ErrProviderFailed) || strings.Contains(err.Error(), unpublished) || strings.Contains(SafeProviderErrorMessage(err), unpublished) {
		t.Fatalf("unsafe error = %v", err)
	}
}
