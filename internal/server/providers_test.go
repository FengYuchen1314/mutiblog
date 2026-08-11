package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

type providerErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func TestProviderErrorContractSeparatesMissingKeyAndInvalidConfiguration(t *testing.T) {
	provider := domain.AIProviderConfig{BaseURL: "https://provider.test/v1", Model: "test-model"}
	_, missingKey := (ai.Client{}).Chat(context.Background(), provider, " \t ", nil, 4)
	_, invalidProvider := (ai.Client{}).Chat(context.Background(), domain.AIProviderConfig{BaseURL: "file:///tmp", Model: "test-model"}, "key", nil, 4)

	for _, test := range []struct {
		name string
		err  error
		code string
	}{
		{name: "missing-key", err: missingKey, code: "provider_key_missing"},
		{name: "invalid-provider", err: invalidProvider, code: "provider_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			(&Server{}).writeProviderError(recorder, test.err)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var response providerErrorResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.code {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestProviderErrorContractExposesOnlySafeUpstreamSummary(t *testing.T) {
	const apiKey = "server-provider-secret"
	const unrelatedKey = "sk-other-server-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Credential rejected for Bearer server-provider-secret; fallback sk-other-server-secret; request tenant disabled"}}`))
	}))
	defer upstream.Close()

	_, providerErr := (ai.Client{HTTPClient: upstream.Client()}).Chat(context.Background(), domain.AIProviderConfig{
		BaseURL: upstream.URL, Model: "test-model",
	}, apiKey, nil, 4)
	recorder := httptest.NewRecorder()
	(&Server{}).writeProviderError(recorder, providerErr)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response providerErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "provider_connection_failed" || !strings.Contains(response.Message, "request tenant disabled") || !strings.Contains(response.Message, "[REDACTED]") {
		t.Fatalf("response = %#v", response)
	}
	if strings.Contains(response.Message, apiKey) || strings.Contains(response.Message, unrelatedKey) || strings.Contains(strings.ToLower(response.Message), "bearer "+apiKey) {
		t.Fatalf("unsafe provider response = %#v", response)
	}
}

func TestProviderErrorContractDoesNotTrustArbitraryWrappedMessages(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).writeProviderError(recorder, errors.Join(ai.ErrProviderFailed, errors.New("Bearer wrapper-secret")))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response providerErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "provider_connection_failed" || response.Message != ai.ErrProviderFailed.Error() || strings.Contains(response.Message, "wrapper-secret") {
		t.Fatalf("unsafe wrapped provider response = %#v", response)
	}
}
